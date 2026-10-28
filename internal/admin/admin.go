// Package admin 是本機管理頁：只聽 127.0.0.1，給電腦前的使用者看狀態、配對手機。
package admin

import (
	"embed"
	"encoding/json"
	"net"
	"net/http"
	"strconv"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
	"github.com/yu-ken0207/local-ai-helper/internal/netinfo"
)

//go:embed page/index.html
var page embed.FS

// AppID 狀態回應裡的識別字，第二次啟動時用來確認佔住連接埠的是不是自己
const AppID = "local-ai-helper"

type Server struct {
	Store   *config.Store
	Version string
	// Quit 管理頁按「結束小幫手」
	Quit func()
	// Addrs 目前的區網位址（測試可以換掉）
	Addrs func() []netinfo.Addr
}

type Status struct {
	App     string         `json:"app"`
	Version string         `json:"version"`
	Name    string         `json:"name"`
	APIPort int            `json:"apiPort"`
	Addrs   []netinfo.Addr `json:"addrs"`
	Devices int            `json:"devices"`
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
	addrs := []netinfo.Addr{}
	if s.Addrs != nil {
		addrs = append(addrs, s.Addrs()...)
	}
	writeJSON(w, Status{App: AppID, Version: s.Version, Name: c.Name, APIPort: c.APIPort, Addrs: addrs, Devices: len(c.Devices)})
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
