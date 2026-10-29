package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/engine"
)

// 手機說 Ollama 的格式；Ollama 的模型照原樣轉給 Ollama，其他引擎的模型在這裡翻譯。

// withBody 先把請求內容讀進來（要看模型名稱決定轉給誰，之後還要原樣轉出去）
func (s *Server) withBody(fn func(w http.ResponseWriter, r *http.Request, body []byte)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request is too large")
			return
		}
		fn(w, r, body)
	})
}

// toOllama 原樣轉給 Ollama
func (s *Server) toOllama(w http.ResponseWriter, r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	s.proxy.ServeHTTP(w, r)
}

// ollamaJSON 跟 Ollama 要 JSON；Ollama 沒開回錯誤
func (s *Server) ollamaJSON(ctx context.Context, path string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.OllamaURL, "/")+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func modelName(body []byte) string {
	var b struct {
		Model string `json:"model"`
		Name  string `json:"name"`
	}
	_ = json.Unmarshal(body, &b)
	if b.Model != "" {
		return b.Model
	}
	return b.Name
}

// route 這個模型是不是其他引擎的；不是就交給 Ollama（回 nil）
func (s *Server) route(ctx context.Context, model string) (engine.Backend, string) {
	if s.Registry == nil || model == "" {
		return nil, ""
	}
	return s.Registry.Route(ctx, model)
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Version string `json:"version"`
	}
	if s.ollamaJSON(r.Context(), "/api/version", &v) == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	// 沒有 Ollama 也可以用別的引擎：回小幫手自己的版本
	writeJSON(w, http.StatusOK, map[string]string{"version": s.Version})
}

// tags 所有引擎的模型合在一起；每個模型多一個 engine 欄位（Ollama 不認得的欄位不影響）
func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	var ollama struct {
		Models []map[string]any `json:"models"`
	}
	ollamaOK := s.ollamaJSON(r.Context(), "/api/tags", &ollama) == nil
	models := []map[string]any{}
	for _, m := range ollama.Models {
		m["engine"], m["engine_name"], m["can_load"] = engine.KindOllama, "Ollama", true
		models = append(models, m)
	}
	others := []engine.Model{}
	if s.Registry != nil {
		others = s.Registry.Models(r.Context())
	}
	if !ollamaOK && len(others) == 0 && !s.anyEngine(r.Context()) {
		writeError(w, http.StatusBadGateway, "no_engine", "no AI engine is running on this computer")
		return
	}
	for _, m := range others {
		models = append(models, map[string]any{
			"name": m.Name, "model": m.Name, "size": m.SizeBytes, "engine": m.Engine, "engine_name": m.EngineName, "display_name": m.DisplayName, "can_load": m.CanLoad,
			"details": map[string]any{"family": m.Family, "parameter_size": m.ParamSize, "quantization_level": m.Quantization},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) anyEngine(ctx context.Context) bool {
	for _, e := range s.Engines(ctx) {
		if e.OK {
			return true
		}
	}
	return false
}

// show 模型的能力（看圖、推理、上下文長度）；其他引擎的模型照 Ollama /api/show 的格式組出來
func (s *Server) show(w http.ResponseWriter, r *http.Request, body []byte) {
	name := modelName(body)
	b, _ := s.route(r.Context(), name)
	if b == nil {
		s.toOllama(w, r, body)
		return
	}
	m, ok := s.Registry.Find(r.Context(), name)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "model not found")
		return
	}
	caps := []string{"completion"}
	if m.Vision {
		caps = append(caps, "vision")
	}
	if m.Tools {
		caps = append(caps, "tools")
	}
	out := map[string]any{
		"capabilities": caps,
		"details":      map[string]any{"family": m.Family, "parameter_size": m.ParamSize, "quantization_level": m.Quantization},
		"model_info":   map[string]any{},
	}
	if m.ContextLength > 0 {
		out["model_info"] = map[string]any{m.Engine + ".context_length": m.ContextLength}
	}
	if levels := engine.ReasoningLevels(m.Reasoning); len(levels) > 0 {
		out["capabilities"] = append(caps, "thinking")
		out["thinking"] = map[string]any{"values": levels, "default": levels[len(levels)/2]}
	}
	writeJSON(w, http.StatusOK, out)
}

// ps 已經載入的模型：Ollama 的加上 LM Studio 的
func (s *Server) ps(w http.ResponseWriter, r *http.Request) {
	var ollama struct {
		Models []map[string]any `json:"models"`
	}
	_ = s.ollamaJSON(r.Context(), "/api/ps", &ollama)
	models := ollama.Models
	if models == nil {
		models = []map[string]any{}
	}
	if s.Registry != nil {
		for _, m := range s.Registry.Models(r.Context()) {
			if m.LoadedContext > 0 {
				models = append(models, map[string]any{"name": m.Name, "model": m.Name, "size_vram": 0, "context_length": m.LoadedContext, "engine": m.Engine})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// ollamaNames Ollama 上的模型名稱（其他引擎撞名時要讓給 Ollama）
func (s *Server) ollamaNames(ctx context.Context) map[string]bool {
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	names := map[string]bool{}
	if s.ollamaJSON(ctx, "/api/tags", &tags) == nil {
		for _, m := range tags.Models {
			names[m.Name] = true
		}
	}
	return names
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request, body []byte) {
	b, original := s.route(r.Context(), modelName(body))
	if b == nil {
		s.toOllama(w, r, body)
		return
	}
	var req engine.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	shown := req.Model
	req.Model = original
	if err := b.Chat(r.Context(), req, &renameWriter{ResponseWriter: w, from: original, to: shown}); err != nil {
		writeEngineError(w, err)
	}
}

// generate 手機只用它預先載入（keep_alive）與卸載（keep_alive: 0）
func (s *Server) generate(w http.ResponseWriter, r *http.Request, body []byte) {
	b, original := s.route(r.Context(), modelName(body))
	if b == nil {
		s.toOllama(w, r, body)
		return
	}
	var req struct {
		Prompt    string `json:"prompt"`
		KeepAlive any    `json:"keep_alive"`
	}
	_ = json.Unmarshal(body, &req)
	if req.Prompt != "" || req.KeepAlive == nil {
		writeEngineError(w, engine.ErrUnsupported)
		return
	}
	var err error
	if isZero(req.KeepAlive) {
		err = b.Unload(r.Context(), original)
	} else {
		err = b.Load(r.Context(), original)
	}
	if err != nil {
		writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": modelName(body), "done": true})
}

func isZero(v any) bool {
	switch x := v.(type) {
	case float64:
		return x == 0
	case string:
		return x == "0" || x == "0s"
	}
	return false
}

func writeEngineError(w http.ResponseWriter, err error) {
	if rw, ok := w.(*renameWriter); ok && rw.wrote {
		return // 已經開始回答，錯誤寫在串流裡了
	}
	var e *engine.Error
	if errors.As(err, &e) {
		code := "engine_error"
		if e == engine.ErrUnsupported {
			code = "unsupported"
		}
		writeError(w, e.Status, code, e.Message)
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	writeError(w, http.StatusBadGateway, "engine_error", err.Error())
}

// renameWriter 引擎回答裡的模型名稱換回手機看到的名稱（撞名時手機看到的是「名稱@引擎」）
type renameWriter struct {
	http.ResponseWriter
	from, to string
	wrote    bool
}

func (rw *renameWriter) WriteHeader(code int) {
	rw.wrote = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *renameWriter) Write(p []byte) (int, error) {
	rw.wrote = true
	if rw.from != rw.to {
		fixed := bytes.ReplaceAll(p, []byte(`"model":"`+rw.from+`"`), []byte(`"model":"`+rw.to+`"`))
		if _, err := rw.ResponseWriter.Write(fixed); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return rw.ResponseWriter.Write(p)
}

func (rw *renameWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
