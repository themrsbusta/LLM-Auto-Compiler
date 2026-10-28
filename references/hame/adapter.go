package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (e *Engine) commonArgs(compiler bool) []string {
	model, device := e.cfg.MainModel, e.cfg.MainDevice
	if compiler {
		model, device = e.cfg.CompilerModel, e.cfg.CompilerDevice
	}
	layers := e.cfg.GPULayers
	if device == "none" {
		layers = "0"
	}
	args := []string{"--model", model, "--ctx-size", fmt.Sprint(e.cfg.Context), "--gpu-layers", layers}
	if device != "" && device != "auto" && (e.profile == nil || e.profile.Flags["--device"]) {
		args = append(args, "--device", device)
	}
	if e.profile != nil && e.profile.Flags["--reasoning"] {
		args = append(args, "--reasoning", "off")
	}
	if !compiler {
		if e.cfg.MainMMProj != "" {
			args = append(args, "--mmproj", e.cfg.MainMMProj)
		}
		args = append(args, e.extraMain...)
	}
	return args
}
func (e *Engine) start(ctx context.Context, compiler bool) (*Worker, error) {
	url, model, port, tag := e.cfg.MainURL, e.cfg.MainModel, e.cfg.PortMain, "main"
	if compiler {
		url, model, port, tag = e.cfg.CompilerURL, e.cfg.CompilerModel, e.cfg.PortCompiler, "compiler"
	}
	if url != "" {
		return &Worker{url: strings.TrimRight(url, "/"), model: model, compiler: compiler}, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if e.cfg.Backend == "cli" {
		return &Worker{cli: true, model: model, compiler: compiler}, nil
	}
	if model == "" {
		return nil, errors.New("modelo não configurado")
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if err = os.MkdirAll(filepath.Join(e.cfg.Data, "logs"), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(e.cfg.Data, "logs", tag+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	args := append(e.commonArgs(compiler), "--host", "127.0.0.1", "--port", fmt.Sprint(port), "--parallel", "1")
	if e.profile != nil && e.profile.Flags["--slots"] {
		args = append(args, "--slots")
	}
	if e.profile != nil {
		for _, flag := range []string{"--no-cache-idle-slots"} {
			if e.profile.Flags[flag] {
				args = append(args, flag)
			}
		}
		if e.profile.Flags["--cache-ram"] {
			args = append(args, "--cache-ram", "0")
		}
	}
	if e.cfg.Mode == "resident" && !compiler {
		slotDir := filepath.Join(e.cfg.Data, "runtime-slots", "main")
		if err = os.MkdirAll(slotDir, 0700); err != nil {
			f.Close()
			return nil, err
		}
		args = append(args, "--slot-save-path", slotDir)
	}
	cmd := exec.Command(e.cfg.Llama, args...)
	cmd.Env = cleanEnv()
	cmd.Stdout = f
	cmd.Stderr = f
	if err = startOwned(cmd); err != nil {
		f.Close()
		return nil, err
	}
	w := &Worker{cmd: cmd, done: make(chan error, 1), logfile: f, url: fmt.Sprintf("http://127.0.0.1:%d", port), model: model, compiler: compiler}
	go func() { w.done <- waitOwned(cmd) }()
	logWorker(tag, cmd.Process.Pid, model)
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-w.done:
			w.cmd = nil
			f.Close()
			return nil, fmt.Errorf("%s encerrou: %v; consulte logs", tag, err)
		case <-ctx.Done():
			w.stop()
			return nil, ctx.Err()
		case <-deadline.C:
			w.stop()
			return nil, errors.New("timeout ao carregar modelo")
		case <-tick.C:
			req, _ := http.NewRequestWithContext(ctx, "GET", w.url+"/health", nil)
			r, er := e.client.Do(req)
			if er == nil {
				r.Body.Close()
				if r.StatusCode == 200 {
					if !compiler {
						e.refreshMetadata(ctx, w)
					}
					return w, nil
				}
			}
		}
	}
}
func logWorker(tag string, pid int, model string) {
	fmt.Printf("worker=%s pid=%d model=%s\n", tag, pid, filepath.Base(model))
}
func (e *Engine) cliGenerate(ctx context.Context, w *Worker, msg []Message, limit int, schemaText string) (string, error) {
	if e.profile == nil {
		return "", errors.New("perfil CLI não validado")
	}
	dir, err := os.MkdirTemp(e.cfg.Data, "cli-task-")
	if os.IsNotExist(err) {
		if err = os.MkdirAll(e.cfg.Data, 0700); err == nil {
			dir, err = os.MkdirTemp(e.cfg.Data, "cli-task-")
		}
	}
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	var system, prompt strings.Builder
	for _, m := range msg {
		if m.Role == "system" {
			system.WriteString(m.Content + "\n")
		} else {
			if prompt.Len() > 0 {
				prompt.WriteString("\n\n" + m.Role + ": ")
			}
			prompt.WriteString(m.Content)
		}
	}
	user := strings.TrimSpace(prompt.String())
	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte(user), 0600)
	os.WriteFile(filepath.Join(dir, "system.txt"), []byte(system.String()), 0600)
	args := append(e.commonArgs(w.compiler), "--file", filepath.Join(dir, "prompt.txt"), "--system-prompt-file", filepath.Join(dir, "system.txt"), "--single-turn", "--simple-io", "--no-escape", "--no-display-prompt", "--log-disable", "--no-show-timings", "--n-predict", fmt.Sprint(limit))
	if e.profile.Flags["--color"] {
		args = append(args, "--color", "off")
	}
	resultPath := filepath.Join(dir, "response.txt")
	args = append(args, "--output-file", resultPath)
	if schemaText != "" {
		if e.profile.Flags["--temperature"] {
			args = append(args, "--temperature", "0")
		} else if e.profile.Flags["--temp"] {
			args = append(args, "--temp", "0")
		}
	}
	cmd := exec.CommandContext(ctx, e.cfg.CLI, args...)
	cmd.Env = cleanEnv()
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	if err = startOwned(cmd); err != nil {
		return "", err
	}
	logWorker("cli", cmd.Process.Pid, w.model)
	if err = waitOwned(cmd); err != nil {
		return "", fmt.Errorf("CLI: %w; %.700s", err, diagnostic.String())
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return "", fmt.Errorf("CLI output file missing: %w; %.500s", err, diagnostic.String())
	}
	return cleanCLIFile(string(data), user)
}
func cleanCLIFile(raw, prompt string) (string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	prompt = strings.ReplaceAll(prompt, "\r\n", "\n")
	prefix := "User:\n" + prompt + "\n\nAssistant:\n"
	if !strings.HasPrefix(raw, prefix) {
		return "", errors.New("formato do arquivo de saída do CLI mudou; use server ou uma versão CLI testada")
	}
	text := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	if strings.HasPrefix(text, "[Start thinking]") {
		if i := strings.Index(text, "[End thinking]"); i >= 0 {
			text = strings.TrimSpace(text[i+len("[End thinking]"):])
		}
	}
	if text == "" {
		return "", errors.New("CLI não gerou resposta; verifique versão, template e modelo")
	}
	return text, nil
}

func cleanCLIOutput(raw, prompt string) (string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	prompt = strings.ReplaceAll(prompt, "\r\n", "\n")
	marker := "> " + prompt + "\n"
	i := strings.Index(raw, marker)
	if i < 0 {
		return "", errors.New("formato de saída do llama-cli mudou: não foi possível separar a resposta; use server ou uma versão CLI testada")
	}
	text := raw[i+len(marker):]
	if at := strings.LastIndex(text, "\nExiting..."); at >= 0 && strings.TrimSpace(text[at:]) == "Exiting..." {
		text = text[:at]
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("resposta CLI vazia")
	}
	return text, nil
}
func (e *Engine) displayModel() string {
	e.metaMu.RLock()
	defer e.metaMu.RUnlock()
	if e.modelName != "" {
		return e.modelName
	}
	if e.cfg.MainModel != "" {
		return filepath.Base(e.cfg.MainModel)
	}
	return "modelo não configurado"
}
func (e *Engine) refreshMetadata(ctx context.Context, w *Worker) {
	req, _ := http.NewRequestWithContext(ctx, "GET", w.url+"/props", nil)
	r, err := e.client.Do(req)
	if err != nil {
		return
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return
	}
	var props map[string]any
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&props) != nil {
		return
	}
	name, _ := props["model_alias"].(string)
	if name == "" {
		name = filepath.Base(e.cfg.MainModel)
	}
	if strings.ContainsAny(name, `\/`) {
		name = filepath.Base(name)
	}
	e.metaMu.Lock()
	e.props = props
	e.modelName = name
	e.metaMu.Unlock()
}
func (e *Engine) metadata(route string) any {
	name := e.displayModel()
	if route != "/props" {
		return map[string]any{"object": "list", "data": []any{map[string]any{"id": name, "name": name, "object": "model", "owned_by": "llamacpp"}}, "models": []any{map[string]any{"name": name, "model": name, "capabilities": []string{"completion"}}}}
	}
	e.metaMu.RLock()
	var props map[string]any
	if e.props != nil {
		b, _ := json.Marshal(e.props)
		json.Unmarshal(b, &props)
	}
	e.metaMu.RUnlock()
	if props == nil {
		props = map[string]any{"default_generation_settings": map[string]any{"n_ctx": e.cfg.Context, "params": map[string]any{"temperature": 0.2, "max_tokens": e.cfg.Output}}, "total_slots": 1, "chat_template": "", "ui": true, "endpoint_slots": false, "endpoint_props": false, "cors_proxy_enabled": false, "is_sleeping": false}
	}
	props["model_alias"] = name
	props["model_path"] = e.cfg.MainModel
	props["build_info"] = "HAME " + Version
	props["ui_settings"] = map[string]any{"model": name}
	if defaults, ok := props["default_generation_settings"].(map[string]any); ok {
		defaults["n_ctx"] = e.cfg.Context
	}
	props["modalities"] = map[string]bool{"vision": e.cfg.MainMMProj != "" && e.cfg.Backend == "server", "audio": false, "video": false}
	return props
}
