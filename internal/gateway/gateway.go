// Package gateway 是手機連的門口（聽區網）：
//   - 不用權杖：/helper/v1/hello（讓 App 找到小幫手）、/helper/v1/pair（用配對碼換權杖）
//   - 要權杖：/helper/v1/info、Ollama 格式的 /api/...（轉給這台電腦上的 Ollama）
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
	"github.com/yu-ken0207/local-ai-helper/internal/engine"
	"github.com/yu-ken0207/local-ai-helper/internal/pairing"
)

// AppID hello 回應裡的識別字，App 用它分辨是不是小幫手
const AppID = "local-ai-helper"

// maxBody 一次最多收多少（訊息裡的照片是 base64，最多 4 張）
const maxBody = 64 << 20

type Server struct {
	proxy   *httputil.ReverseProxy
	Store   *config.Store
	Pairing *pairing.Manager
	Version string
	// OllamaURL 這台電腦上的 Ollama，例如 http://127.0.0.1:11434
	OllamaURL string
	// Registry 其他引擎（LM Studio、OpenAI 格式的伺服器）；nil 就只用 Ollama
	Registry *engine.Registry
}

type Hello struct {
	App     string `json:"app"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Pairing bool   `json:"pairing"`
}

type Info struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Version string        `json:"version"`
	Device  string        `json:"device"`
	Engines []engine.Info `json:"engines"`
	// Features 手機可以用的功能（不同引擎做得到的不一樣）
	Features map[string]bool `json:"features"`
}

func (s *Server) Handler() http.Handler {
	target, err := url.Parse(s.OllamaURL)
	if err != nil {
		panic(err)
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
			// 權杖只給小幫手看，不往後送
			r.Out.Header.Del("Authorization")
		},
		// 串流回答一收到就送出去
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, context.Canceled) {
				return
			}
			writeError(w, http.StatusBadGateway, "engine_unreachable", "Ollama is not running on this computer")
		},
	}
	s.proxy = proxy
	if s.Registry != nil && s.Registry.Reserved == nil {
		s.Registry.Reserved = s.ollamaNames
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /helper/v1/hello", s.hello)
	mux.HandleFunc("POST /helper/v1/pair", s.pair)
	mux.Handle("GET /helper/v1/info", s.auth(http.HandlerFunc(s.info)))
	mux.Handle("GET /api/version", s.auth(http.HandlerFunc(s.version)))
	mux.Handle("GET /api/tags", s.auth(http.HandlerFunc(s.tags)))
	mux.Handle("POST /api/show", s.auth(s.withBody(s.show)))
	mux.Handle("GET /api/ps", s.auth(http.HandlerFunc(s.ps)))
	mux.Handle("POST /api/chat", s.auth(s.withBody(s.chat)))
	mux.Handle("POST /api/generate", s.auth(s.withBody(s.generate)))
	mux.Handle("POST /api/pull", s.auth(s.withBody(func(w http.ResponseWriter, r *http.Request, body []byte) { s.toOllama(w, r, body) })))
	// 其他 Ollama API（刪除、建立、推送模型…）一律不轉
	mux.Handle("/api/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_allowed", "this API is not available through the helper")
	})))
	return mux
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	c := s.Store.Get()
	_, pairing := s.Pairing.Active()
	writeJSON(w, http.StatusOK, Hello{App: AppID, ID: c.ID, Name: c.Name, Version: s.Version, Pairing: pairing})
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"deviceName"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	token, dev, err := s.Pairing.Pair(body.Code, body.DeviceName)
	switch {
	case errors.Is(err, pairing.ErrNotPairing):
		writeError(w, http.StatusConflict, "not_pairing", "pairing is not open on this computer")
	case errors.Is(err, pairing.ErrBadCode):
		writeError(w, http.StatusForbidden, "bad_code", "wrong pairing code")
	case errors.Is(err, pairing.ErrTooMany):
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "too many wrong codes; start pairing again on the computer")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	default:
		c := s.Store.Get()
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "deviceId": dev.ID, "helper": map[string]string{"id": c.ID, "name": c.Name}})
	}
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	c := s.Store.Get()
	dev, _ := r.Context().Value(deviceKey{}).(config.Device)
	engines := s.Engines(r.Context())
	features := map[string]bool{"pull": false, "preload": false}
	for _, e := range engines {
		if e.OK && e.Kind == engine.KindOllama {
			features["pull"], features["preload"] = true, true
		}
		if e.OK && e.Kind == engine.KindLMStudio {
			features["preload"] = true
		}
	}
	writeJSON(w, http.StatusOK, Info{ID: c.ID, Name: c.Name, Version: s.Version, Device: dev.Name, Engines: engines, Features: features})
}

// Engines 這台電腦上的 AI 引擎狀態：Ollama（一定列出，沒開也列，讓使用者知道要開）＋偵測到的其他引擎
func (s *Server) Engines(ctx context.Context) []engine.Info {
	out := []engine.Info{s.ollamaStatus(ctx)}
	if s.Registry != nil {
		out = append(out, s.Registry.Infos(ctx)...)
	}
	return out
}

// ollamaStatus 問 Ollama 的版本，問得到就是開著
func (s *Server) ollamaStatus(ctx context.Context) engine.Info {
	e := engine.Info{ID: engine.KindOllama, Kind: engine.KindOllama, Name: "Ollama", URL: s.OllamaURL}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.OllamaURL, "/")+"/api/version", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return e
	}
	defer resp.Body.Close()
	var v struct {
		Version string `json:"version"`
	}
	if resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&v) == nil {
		e.OK, e.Version = true, v.Version
	}
	return e
}

type deviceKey struct{}

// auth 要帶配對時拿到的權杖（Authorization: Bearer ...）
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "not_paired", "pair this phone first")
			return
		}
		dev, ok := s.Pairing.Authenticate(strings.TrimSpace(token))
		if !ok {
			writeError(w, http.StatusUnauthorized, "not_paired", "this phone is not paired (or was removed)")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), deviceKey{}, dev)))
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError 錯誤用 Ollama 的格式 {"error": "..."}，再加一個代碼讓 App 判斷
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
