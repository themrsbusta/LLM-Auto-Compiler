package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed all:webui
var assets embed.FS

const Version = "0.92 RC"
const CompiledSchema = `{"type":"object","properties":{"system_instruction":{"type":"string","minLength":1},"context_bundle":{"type":"object","properties":{"retrieved_knowledge":{"type":"array","items":{"type":"object","properties":{"source":{"type":"string"},"fact":{"type":"string"}},"required":["source","fact"],"additionalProperties":false}},"conversation_lineage":{"type":"array","items":{"type":"string"}}},"required":["retrieved_knowledge","conversation_lineage"],"additionalProperties":false},"response_format":{"type":"string","minLength":1}},"required":["system_instruction","context_bundle","response_format"],"additionalProperties":false}`

type Config struct {
	Capabilities       []string `json:"llama_capabilities,omitempty"`
	RunnerVersion      string   `json:"llama_version,omitempty"`
	MainMMProj         string   `json:"main_mmproj,omitempty"`
	MainExtraArgs      string   `json:"main_extra_args,omitempty"`
	Threshold          int      `json:"compaction_threshold_percent"`
	Reserve            int      `json:"reserve_tokens"`
	TemplateReserve    int      `json:"template_reserve_tokens"`
	Policy             string   `json:"memory_policy"`
	Prefill            *bool    `json:"prefill_compiled_context,omitempty"`
	MemoryMax          int      `json:"compiled_memory_max_tokens"`
	Tokenizer          string   `json:"llama_tokenize,omitempty"`
	Backend            string   `json:"backend"`
	CLI                string   `json:"llama_cli"`
	MainDevice         string   `json:"main_device"`
	CompilerDevice     string   `json:"compiler_device"`
	MainDeviceName     string   `json:"main_device_name,omitempty"`
	CompilerDeviceName string   `json:"compiler_device_name,omitempty"`
	PortMain           int      `json:"main_port,omitempty"`
	PortCompiler       int      `json:"compiler_port,omitempty"`
	Mode               string   `json:"mode"`
	Llama              string   `json:"llama_server"`
	MainModel          string   `json:"main_model"`
	CompilerModel      string   `json:"compiler_model"`
	Data               string   `json:"data_dir"`
	Listen             string   `json:"listen"`
	MainURL            string   `json:"main_url"`
	CompilerURL        string   `json:"compiler_url"`
	Context            int      `json:"context_tokens"`
	Output             int      `json:"output_tokens"`
	GPULayers          string   `json:"gpu_layers"`
}
type State struct {
	SessionID          string `json:"session_id"`
	CurrentGoal        string `json:"current_goal"`
	UnresolvedQuestion string `json:"unresolved_question"`
	LastSummary        string `json:"last_summary"`
	SearchIntent       string `json:"search_intent"`
}
type Fact struct {
	Source string `json:"source"`
	Fact   string `json:"fact"`
}
type Bundle struct {
	Knowledge []Fact   `json:"retrieved_knowledge"`
	Lineage   []string `json:"conversation_lineage"`
}
type Compiled struct {
	System string `json:"system_instruction"`
	Bundle Bundle `json:"context_bundle"`
	Format string `json:"response_format"`
}
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Session struct {
	Messages    []Message         `json:"messages"`
	Summary     string            `json:"summary"`
	Compiled    *Compiled         `json:"compiled,omitempty"`
	Sources     map[string]string `json:"compiled_sources,omitempty"`
	Compactions int               `json:"compactions"`
	LastArchive string            `json:"last_archive,omitempty"`
	Usage       TokenUsage        `json:"context_usage"`
}
type Worker struct {
	cmd              *exec.Cmd
	done             chan error
	logfile          *os.File
	url              string
	cli              bool
	model            string
	compiler         bool
	lastPromptTokens int
}
type Engine struct {
	lifecycle      context.Context
	cfg            Config
	mu             sync.Mutex
	main, compiler *Worker
	client         *http.Client
	pending        chan struct{}
	profile        *Profile
	extraMain      []string
	metaMu         sync.RWMutex
	props          map[string]any
	modelName      string
	activeSession  string
	audit          []AuditEvent
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func atomicJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(path), ".hame-*")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, e = tmp.Write(b); e != nil {
		tmp.Close()
		return e
	}
	if e = tmp.Sync(); e != nil {
		tmp.Close()
		return e
	}
	if e = tmp.Close(); e != nil {
		return e
	}
	return os.Rename(name, path)
}
func (e *Engine) path(id, suffix string) string {
	return filepath.Join(e.cfg.Data, "sessions", id+suffix)
}
func (w *Worker) stop() {
	if w == nil || w.cmd == nil {
		return
	}
	_ = killOwned(w.cmd)
	<-w.done
	_ = w.logfile.Close()
	w.cmd = nil
}
func (w *Worker) exited() bool {
	if w == nil || w.cmd == nil || w.done == nil {
		return false
	}
	select {
	case <-w.done:
		w.cmd = nil
		if w.logfile != nil {
			w.logfile.Close()
		}
		return true
	default:
		return false
	}
}
func (e *Engine) close() { e.mu.Lock(); defer e.mu.Unlock(); e.main.stop(); e.compiler.stop() }
func (e *Engine) ensure(ctx context.Context, compiler bool) (*Worker, error) {
	if e.main != nil && e.main.exited() {
		e.main = nil
		e.record("worker_exited", "main detected dead; next ensure reloads", 0, 0, 0)
	}
	if e.compiler != nil && e.compiler.exited() {
		e.compiler = nil
		e.record("worker_exited", "compiler detected dead; next ensure reloads", 0, 0, 0)
	}

	if compiler {
		if e.cfg.Mode == "constrained" {
			e.main.stop()
			e.main = nil
		}
		if e.compiler == nil {
			w, err := e.start(ctx, true)
			if err != nil {
				return nil, err
			}
			e.compiler = w
		}
		return e.compiler, nil
	}
	if e.cfg.Mode == "constrained" {
		e.compiler.stop()
		e.compiler = nil
	}
	if e.main == nil {
		w, err := e.start(ctx, false)
		if err != nil {
			return nil, err
		}
		e.main = w
	}
	return e.main, nil
}
func (e *Engine) completion(ctx context.Context, w *Worker, msg []Message, structured bool) (string, error) {
	schema := ""
	limit := e.cfg.Output
	if structured {
		schema = CompiledSchema
		limit = e.memoryLimit()
	}
	return e.generate(ctx, w, msg, limit, schema)
}
func (e *Engine) generate(ctx context.Context, w *Worker, msg []Message, limit int, schemaText string) (string, error) {
	if w.cli {
		return e.cliGenerate(ctx, w, msg, limit, schemaText)
	}
	body := map[string]any{"messages": msg, "max_tokens": limit, "temperature": 0.2, "cache_prompt": true, "id_slot": 0}
	if schemaText != "" {
		var schema any
		json.Unmarshal([]byte(schemaText), &schema)
		body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "compiled_prompt", "strict": true, "schema": schema}}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", w.url+"/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	r, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if r.StatusCode != 200 {
		return "", fmt.Errorf("LLM HTTP %d: %.500s", r.StatusCode, data)
	}
	var response struct {
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err = json.Unmarshal(data, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 || response.Choices[0].Message.Content == "" {
		return "", errors.New("resposta LLM vazia")
	}
	w.lastPromptTokens = response.Usage.PromptTokens
	return response.Choices[0].Message.Content, nil
}
func decodeContract(s string, v any) error {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		i := strings.Index(s, "\n")
		if i < 0 {
			return errors.New("JSON inválido")
		}
		s = strings.TrimSuffix(strings.TrimSpace(s[i+1:]), "```")
	}
	return json.Unmarshal([]byte(s), v)
}
func (e *Engine) archive(id string, msg Message) error {
	dir := filepath.Join(e.cfg.Data, "archive", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(dir, fmt.Sprintf("%020d.json", time.Now().UnixNano())), msg)
}
func main() {
	exe, _ := os.Executable()
	config := flag.String("config", filepath.Join(filepath.Dir(exe), "config.json"), "configuration file")
	configure := flag.Bool("configure", false, "open native configurator")
	check := flag.Bool("check", false, "validate configuration and capabilities")
	probePath := flag.String("probe", "", "inspect selected llama executable as JSON")
	runner := flag.String("backend", "server", "server or cli for probe")
	autoOpen := flag.Bool("open", !manualPlatform, "open chat when ready")
	version := flag.Bool("version", false, "show HAME version")
	testMode := flag.Bool("test-mode", false, "load models and verify execution mode")
	compactFlag := flag.Bool("compact", false, "compact current or selected session")
	recoveryFlag := flag.Bool("recover", false, "retrieve full original archive during manual compaction")
	memoryStatus := flag.Bool("memory-status", false, "query active HAME context status")
	sessionFlag := flag.String("session", "", "session id; empty selects current session")
	flag.Parse()
	defer closeOwnedJob()
	if *version {
		fmt.Println("HAME " + Version)
		return
	}
	if *probePath != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		p, err := probe(ctx, *probePath, *runner)
		if err != nil {
			json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
			os.Exit(1)
		}
		json.NewEncoder(os.Stdout).Encode(p)
		return
	}
	if *configure {
		if manualPlatform {
			log.Print("Linux/Termux: edite config.json; configurador indisponível")
			os.Exit(1)
		}
		if err := showConfigurator(*config); err != nil {
			log.Print(err)
		}
		return
	}
	c, err := load(*config)
	if (*compactFlag || *memoryStatus) && err == nil {
		if er := memoryClient(c, *sessionFlag, *compactFlag, *recoveryFlag); er != nil {
			log.Print(er)
			os.Exit(1)
		}
		return
	}
	if err != nil {
		if *check || manualPlatform {
			log.Print(err)
			os.Exit(1)
		}
		log.Printf("Configuração necessária: %v", err)
		if er := showConfigurator(*config); er != nil {
			log.Print(er)
			return
		}
		c, err = load(*config)
		if err != nil {
			log.Print(err)
			return
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()
	removeHandler, err := installConsoleHandler(stop)
	if err != nil {
		log.Print(err)
		return
	}
	defer removeHandler()
	setConsoleTitle("HAME " + Version + " — feche esta janela para encerrar os modelos")
	e := &Engine{cfg: c, client: &http.Client{Timeout: 3 * time.Minute}, lifecycle: ctx}
	if err = e.validate(ctx); err != nil {
		log.Printf("Configuração/compatibilidade: %v", err)
		if *check || manualPlatform {
			os.Exit(1)
		}
		if er := showConfigurator(*config); er != nil {
			log.Print(er)
			return
		}
		c, err = load(*config)
		if err != nil {
			log.Print(err)
			return
		}
		e.cfg = c
		if err = e.validate(ctx); err != nil {
			log.Print(err)
			return
		}
	}
	if *check {
		fmt.Println("Configuração válida — " + e.profile.Version)
		return
	}
	defer e.close()
	if err = e.initialize(ctx, *testMode); err != nil {
		log.Printf("Modo incompatível: %v", err)
		e.close()
		os.Exit(1)
	}
	if *testMode {
		if err = e.testExecutionMode(ctx); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"mode": c.Mode, "backend": c.Backend, "verified": true, "audit": e.audit})
		return
	}
	e.modelName = filepath.Base(c.MainModel)
	srv := &http.Server{Addr: c.Listen, Handler: e.handler(), ReadHeaderTimeout: 10 * time.Second}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		log.Print(err)
		return
	}
	defer listener.Close()
	go func() { <-ctx.Done(); _ = srv.Close() }()
	log.Printf("HAME %s | motor=%s | modelo=%s | %s", Version, c.Backend, e.displayModel(), c.Listen)
	log.Print("Ctrl+C ou fechar esta janela: encerra HAME e apenas os workers que ele iniciou.")
	if *autoOpen {
		openBrowser("http://" + c.Listen)
	}
	err = srv.Serve(listener)
	stop()
	e.close()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Print(err)
	}
}
