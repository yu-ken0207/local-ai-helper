// Package admin 是本機管理頁：只聽 127.0.0.1，給電腦前的使用者看狀態、配對手機。
package admin

import (
	"context"
	"embed"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
	"github.com/yu-ken0207/local-ai-helper/internal/engine"
	"github.com/yu-ken0207/local-ai-helper/internal/netinfo"
	"github.com/yu-ken0207/local-ai-helper/internal/ollama"
	"github.com/yu-ken0207/local-ai-helper/internal/pairing"
)

//go:embed page/index.html
var page embed.FS

// AppID 狀態回應裡的識別字，第二次啟動時用來確認佔住連接埠的是不是自己
const AppID = "local-ai-helper"

type Server struct {
	Store   *config.Store
	Pairing *pairing.Manager
	Version string
	// Engines 這台電腦上的 AI 引擎狀態
	Engines func(ctx context.Context) []engine.Info
	// Redetect 重新尋找引擎（管理頁按「重新尋找」）
	Redetect func()
	// APIError 手機用的連接埠開不起來時的原因（例如被別的程式佔用）
	APIError func() string
	// Quit 管理頁按「結束小幫手」
	Quit func()
	// Addrs 目前的區網位址（測試可以換掉）
	Addrs func() []netinfo.Addr
	// Ollama 幫忙裝、開 Ollama；nil 就不提供
	Ollama *ollama.Manager
}

type Status struct {
	App      string         `json:"app"`
	Version  string         `json:"version"`
	Name     string         `json:"name"`
	APIPort  int            `json:"apiPort"`
	APIError string         `json:"apiError,omitempty"`
	Addrs    []netinfo.Addr `json:"addrs"`
	Devices  []DeviceView   `json:"devices"`
	Pairing  *PairingView   `json:"pairing,omitempty"`
	Paired   *DeviceView    `json:"paired,omitempty"`
	Engines  []engine.Info  `json:"engines"`
	Ollama   *ollama.Status `json:"ollama,omitempty"`
}

type DeviceView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	PairedAt   time.Time  `json:"pairedAt"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

// PairingView 配對時段：配對碼、到期時間、QR code（data URL）
type PairingView struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
	QR        string    `json:"qr"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		data, _ := page.ReadFile("page/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /admin/api/status", s.status)
	mux.HandleFunc("POST /admin/api/name", s.rename)
	mux.HandleFunc("POST /admin/api/pairing", func(w http.ResponseWriter, r *http.Request) {
		s.Pairing.Start()
		s.status(w, r)
	})
	mux.HandleFunc("POST /admin/api/pairing/stop", func(w http.ResponseWriter, r *http.Request) {
		s.Pairing.Stop()
		s.status(w, r)
	})
	mux.HandleFunc("POST /admin/api/devices/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		if !s.Pairing.Revoke(r.PathValue("id")) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.status(w, r)
	})
	mux.HandleFunc("POST /admin/api/servers", s.addServer)
	mux.HandleFunc("POST /admin/api/servers/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		found := false
		_ = s.Store.Update(func(c *config.Config) {
			kept := c.Servers[:0]
			for _, sv := range c.Servers {
				if sv.ID == id {
					found = true
					continue
				}
				kept = append(kept, sv)
			}
			c.Servers = kept
		})
		if !found {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.redetect()
		s.status(w, r)
	})
	mux.HandleFunc("POST /admin/api/engines/redetect", func(w http.ResponseWriter, r *http.Request) {
		s.redetect()
		s.status(w, r)
	})
	mux.HandleFunc("POST /admin/api/ollama/install", s.installOllama)
	mux.HandleFunc("POST /admin/api/ollama/start", s.startOllama)
	mux.HandleFunc("POST /admin/api/quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
		if s.Quit != nil {
			go s.Quit()
		}
	})
	return guard(mux)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	c := s.Store.Get()
	st := Status{App: AppID, Version: s.Version, Name: c.Name, APIPort: c.APIPort, Addrs: []netinfo.Addr{}, Devices: []DeviceView{}, Engines: []engine.Info{}}
	if s.Addrs != nil {
		st.Addrs = append(st.Addrs, s.Addrs()...)
	}
	if s.APIError != nil {
		st.APIError = s.APIError()
	}
	for _, d := range c.Devices {
		st.Devices = append(st.Devices, view(d))
	}
	if s.Pairing != nil {
		if w, ok := s.Pairing.Active(); ok {
			ips := make([]string, 0, len(st.Addrs))
			for _, a := range st.Addrs {
				ips = append(ips, a.IP)
			}
			qr, _ := pairing.QRDataURL(pairing.Payload(c.ID, c.Name, c.APIPort, w.Code, ips))
			st.Pairing = &PairingView{Code: w.Code, ExpiresAt: w.ExpiresAt, QR: qr}
		}
		if d := s.Pairing.LastPaired(); d != nil {
			v := view(*d)
			st.Paired = &v
		}
	}
	if s.Engines != nil {
		st.Engines = s.Engines(r.Context())
	}
	if s.Ollama != nil {
		o := s.Ollama.Status()
		st.Ollama = &o
	}
	writeJSON(w, st)
}

func (s *Server) ollamaUp(r *http.Request) bool {
	if s.Engines == nil {
		return false
	}
	for _, e := range s.Engines(r.Context()) {
		if e.Kind == engine.KindOllama && e.OK {
			return true
		}
	}
	return false
}

// installOllama 一鍵安裝；已經開著或使用者自己裝過就不裝（不蓋掉別人的 Ollama）
func (s *Server) installOllama(w http.ResponseWriter, r *http.Request) {
	if s.Ollama == nil {
		writeError(w, http.StatusNotFound, "unsupported", "not available")
		return
	}
	if s.ollamaUp(r) {
		writeError(w, http.StatusConflict, "already_running", "Ollama is already running")
		return
	}
	if s.Ollama.Status().Installed == "system" {
		writeError(w, http.StatusConflict, "already_installed", "Ollama is already installed on this computer")
		return
	}
	s.Ollama.Install()
	s.status(w, r)
}

func (s *Server) startOllama(w http.ResponseWriter, r *http.Request) {
	if s.Ollama == nil {
		writeError(w, http.StatusNotFound, "unsupported", "not available")
		return
	}
	if err := s.Ollama.Start(); err != nil {
		writeError(w, http.StatusBadGateway, "start_failed", err.Error())
		return
	}
	s.status(w, r)
}

func (s *Server) redetect() {
	if s.Redetect != nil {
		s.Redetect()
	}
}

// addServer 加一台 OpenAI 格式的伺服器：網址要是 http(s)://主機:連接埠
func (s *Server) addServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u, ok := normalizeServerURL(body.URL)
	if !ok || len([]rune(body.Name)) > 40 {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}
	if err := s.Store.Update(func(c *config.Config) {
		c.Servers = append(c.Servers, config.Server{ID: config.NewID(6), Name: strings.TrimSpace(body.Name), URL: u, APIKey: strings.TrimSpace(body.APIKey)})
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redetect()
	s.status(w, r)
}

// normalizeServerURL 「127.0.0.1:8080」「http://127.0.0.1:8080/v1/」都整理成 http://127.0.0.1:8080
func normalizeServerURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}

func view(d config.Device) DeviceView {
	return DeviceView{ID: d.ID, Name: d.Name, PairedAt: d.PairedAt, LastSeenAt: d.LastSeenAt}
}

func (s *Server) rename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || !validName(body.Name) {
		http.Error(w, "bad name", http.StatusBadRequest)
		return
	}
	if err := s.Store.Update(func(c *config.Config) { c.Name = body.Name }); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func validName(name string) bool {
	n := len([]rune(name))
	return n >= 1 && n <= 40
}

// guard 擋掉兩種從瀏覽器來的攻擊：
//   - DNS rebinding：網址的主機名稱一定要是 127.0.0.1 或 localhost
//   - 跨網站送出：會改東西的請求要帶 X-Local-AI 標頭（別的網站送不出這個標頭，我們也不回應 CORS 預檢）
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Local-AI") != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// URL 管理頁網址
func URL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/"
}

// writeError {"error": "說明", "code": "代碼"}
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "code": code})
}
