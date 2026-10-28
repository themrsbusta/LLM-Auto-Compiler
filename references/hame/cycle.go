package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (e *Engine) backgroundContext() context.Context {
	if e.lifecycle != nil {
		return e.lifecycle
	}
	return context.Background()
}

type CycleStatus struct {
	Session string     `json:"session_id"`
	Stage   string     `json:"stage"`
	Error   string     `json:"error,omitempty"`
	Updated string     `json:"updated"`
	Mode    string     `json:"mode"`
	Usage   TokenUsage `json:"context_usage"`
}

func (e *Engine) status(id, stage string, err error) {
	e.metaMu.Lock()
	e.activeSession = id
	e.metaMu.Unlock()
	fmt.Printf("HAME session=%s stage=%s error=%v\n", id, stage, err)
	var previous CycleStatus
	b, _ := os.ReadFile(e.path(id, ".status.json"))
	_ = json.Unmarshal(b, &previous)
	s := CycleStatus{Session: id, Stage: stage, Updated: time.Now().Format(time.RFC3339), Mode: e.cfg.Mode, Usage: previous.Usage}
	if err != nil {
		s.Error = err.Error()
	}
	if er := atomicJSON(e.path(id, ".status.json"), s); er != nil {
		fmt.Println("status:", er)
	}
}
func (e *Engine) acquire(ctx context.Context) error {
	for {
		e.mu.Lock()
		if e.pending == nil {
			return nil
		}
		done := e.pending
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
}
func (e *Engine) prime(ctx context.Context, s Session) error {
	if e.cfg.Backend == "cli" {
		return nil
	}
	w, err := e.ensure(ctx, false)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]any{"messages": s.Messages, "add_generation_prompt": true})
	req, _ := http.NewRequestWithContext(ctx, "POST", w.url+"/apply-template", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("apply-template HTTP %d", r.StatusCode)
	}
	var v struct {
		Prompt string `json:"prompt"`
	}
	if err = json.NewDecoder(r.Body).Decode(&v); err != nil {
		return err
	}
	if v.Prompt == "" {
		return errors.New("empty compiled template")
	}
	b, _ = json.Marshal(map[string]any{"prompt": v.Prompt, "n_predict": 0, "cache_prompt": true, "id_slot": 0})
	req, _ = http.NewRequestWithContext(ctx, "POST", w.url+"/completion", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r2, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer r2.Body.Close()
	io.Copy(io.Discard, r2.Body)
	if r2.StatusCode != 200 {
		return fmt.Errorf("prefill HTTP %d", r2.StatusCode)
	}
	return nil
}
func (e *Engine) history(id string) ([]Message, error) {
	var result []Message
	for _, root := range []string{filepath.Join(e.cfg.Data, "archive", id), filepath.Join(e.cfg.Data, "context", id)} {
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			b, er := os.ReadFile(filepath.Join(root, entry.Name()))
			if er != nil {
				return nil, er
			}
			var msgs []Message
			if er = json.Unmarshal(b, &msgs); er != nil {
				var m Message
				if er = json.Unmarshal(b, &m); er != nil {
					return nil, er
				}
				msgs = []Message{m}
			}
			result = append(result, msgs...)
		}
	}
	if result == nil {
		result = []Message{}
	}
	return result, nil
}
func (e *Engine) handler() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(assets, "webui")
	fileServer := http.FileServer(http.FS(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, err := fs.Stat(web, strings.TrimPrefix(r.URL.Path, "/")); err == nil && r.URL.Path != "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		b, _ := assets.ReadFile("webui/index.html")
		w.Write(b)
	})
	mux.HandleFunc("/hame/get_datetime", datetimeHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": Version, "memory_cycle": "saturation_or_retrieval"})
	})
	for _, route := range []string{"/props", "/v1/models", "/models"} {
		route := route
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(e.metadata(route))
		})
	}
	mux.HandleFunc("/hame/runtime", e.runtimeHTTP)
	mux.HandleFunc("/hame/compact", e.compactHTTP)
	mux.HandleFunc("/hame/history", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("session_id")
		if !safeID.MatchString(id) {
			http.Error(w, "invalid session", 400)
			return
		}
		h, err := e.history(id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h)
	})
	mux.HandleFunc("/hame/status", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("session_id")
		if !safeID.MatchString(id) {
			http.Error(w, "invalid session", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		b, err := os.ReadFile(e.path(id, ".status.json"))
		if os.IsNotExist(err) {
			io.WriteString(w, `{"stage":"ready"}`)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(b)
	})
	mux.HandleFunc("/v1/streams/lookup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		io.WriteString(w, `{"error":{"message":"no resumable stream"}}`)
	})
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"tools":[]}`)
	})
	mux.HandleFunc("/slots", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "[]") })
	mux.HandleFunc("/hame/media", e.mediaHTTP)
	mux.HandleFunc("/v1/chat/completions", e.chatHTTP)
	return mux
}
func (e *Engine) chatHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var q struct {
		Compile      bool                `json:"compile"`
		Retrieve     bool                `json:"retrieve"`
		Session      string              `json:"session_id"`
		Messages     []mediaInputMessage `json:"messages"`
		Stream       bool                `json:"stream"`
		Conversation string              `json:"conversation_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&q); err != nil {
		http.Error(w, "invalid JSON or unsupported multimodal message", 400)
		return
	}
	if len(q.Messages) == 0 || q.Messages[len(q.Messages)-1].Role != "user" {
		http.Error(w, "last message must be user", 400)
		return
	}
	if q.Session == "" {
		identity := r.Header.Get("X-Conversation-Id")
		if identity == "" {
			identity = q.Conversation
		}
		if identity != "" {
			q.Session = fmt.Sprintf("ui_%x", sha256.Sum256([]byte(identity)))[:67]
		}
	}
	if !safeID.MatchString(q.Session) {
		http.Error(w, "session_id or X-Conversation-Id required", 400)
		return
	}
	user, err := e.acceptMedia(r.Context(), q.Session, q.Messages[len(q.Messages)-1].Content)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	reply, err := e.chatRequest(r.Context(), q.Session, user, q.Compile, q.Retrieve)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": err.Error()}})
		return
	}
	id := fmt.Sprintf("hame-%d", time.Now().UnixNano())
	if q.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		for _, delta := range []map[string]string{{"role": "assistant", "content": reply}, {}} {
			finish := any(nil)
			if len(delta) == 0 {
				finish = "stop"
			}
			b, _ := json.Marshal(map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": e.displayModel(), "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		io.WriteString(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id, "object": "chat.completion", "model": e.displayModel(), "choices": []any{map[string]any{"index": 0, "message": Message{"assistant", reply}, "finish_reason": "stop"}}})
}
