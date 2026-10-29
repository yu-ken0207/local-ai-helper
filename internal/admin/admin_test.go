package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
	"github.com/yu-ken0207/local-ai-helper/internal/engine"
	"github.com/yu-ken0207/local-ai-helper/internal/netinfo"
	"github.com/yu-ken0207/local-ai-helper/internal/ollama"
	"github.com/yu-ken0207/local-ai-helper/internal/pairing"
)

func newServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store, Pairing: pairing.New(store), Version: "test", Addrs: func() []netinfo.Addr { return []netinfo.Addr{{IP: "192.168.1.20", Kind: "lan"}} }}
	return s, s.Handler()
}

func do(h http.Handler, method, host, path, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = host
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPageAndStatus(t *testing.T) {
	_, h := newServer(t)
	if rec := do(h, "GET", "127.0.0.1:11481", "/", "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Local AI") {
		t.Fatalf("管理頁 %d", rec.Code)
	}
	rec := do(h, "GET", "localhost:11481", "/admin/api/status", "", nil)
	var st Status
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.App != AppID || st.Version != "test" || st.APIPort != config.DefaultAPIPort || len(st.Addrs) != 1 || st.Devices == nil || st.Pairing != nil {
		t.Fatalf("狀態不對：%+v", st)
	}
}

func TestRejectsOtherHosts(t *testing.T) {
	_, h := newServer(t)
	// DNS rebinding：惡意網域解析到 127.0.0.1，主機名稱就不會是 127.0.0.1／localhost
	for _, host := range []string{"evil.example:11481", "192.168.1.20:11481"} {
		if rec := do(h, "GET", host, "/admin/api/status", "", nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s 應該被擋，結果 %d", host, rec.Code)
		}
	}
}

func TestChangesNeedHeader(t *testing.T) {
	s, h := newServer(t)
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/name", `{"name":"x"}`, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("沒有 X-Local-AI 標頭應該被擋，結果 %d", rec.Code)
	}
	hdr := map[string]string{"X-Local-AI": "1"}
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/name", `{"name":"書房電腦"}`, hdr); rec.Code != 200 {
		t.Fatalf("改名 %d", rec.Code)
	}
	if s.Store.Get().Name != "書房電腦" {
		t.Fatal("名字沒改到")
	}
	for _, bad := range []string{`{"name":""}`, `{"name":"` + strings.Repeat("長", 41) + `"}`, `壞掉`} {
		if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/name", bad, hdr); rec.Code != http.StatusBadRequest {
			t.Errorf("%s 應該回 400，結果 %d", bad, rec.Code)
		}
	}
}

func TestQuit(t *testing.T) {
	s, h := newServer(t)
	done := make(chan struct{})
	s.Quit = func() { close(done) }
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/quit", "", map[string]string{"X-Local-AI": "1"}); rec.Code != 200 {
		t.Fatalf("結束 %d", rec.Code)
	}
	<-done
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) Status {
	t.Helper()
	var st Status
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatalf("%d %v", rec.Code, err)
	}
	return st
}

func TestPairingFromAdminPage(t *testing.T) {
	s, h := newServer(t)
	hdr := map[string]string{"X-Local-AI": "1"}
	st := decode(t, do(h, "POST", "127.0.0.1:11481", "/admin/api/pairing", "", hdr))
	if st.Pairing == nil || len(st.Pairing.Code) != 6 || !strings.HasPrefix(st.Pairing.QR, "data:image/png;base64,") {
		t.Fatalf("開始配對要回配對碼與 QR code：%+v", st.Pairing)
	}
	// 手機配對成功後，管理頁看得到「已配對」與清單
	if _, _, err := s.Pairing.Pair(st.Pairing.Code, "媽媽的 iPhone"); err != nil {
		t.Fatal(err)
	}
	st = decode(t, do(h, "GET", "127.0.0.1:11481", "/admin/api/status", "", nil))
	if st.Pairing != nil || st.Paired == nil || st.Paired.Name != "媽媽的 iPhone" || len(st.Devices) != 1 {
		t.Fatalf("配對後狀態不對：%+v", st)
	}
	// 移除
	id := st.Devices[0].ID
	st = decode(t, do(h, "POST", "127.0.0.1:11481", "/admin/api/devices/"+id+"/remove", "", hdr))
	if len(st.Devices) != 0 {
		t.Fatal("移除後清單要變空")
	}
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/devices/"+id+"/remove", "", hdr); rec.Code != http.StatusNotFound {
		t.Fatalf("移除不存在的手機：%d", rec.Code)
	}
	// 取消配對
	decode(t, do(h, "POST", "127.0.0.1:11481", "/admin/api/pairing", "", hdr))
	if st := decode(t, do(h, "POST", "127.0.0.1:11481", "/admin/api/pairing/stop", "", hdr)); st.Pairing != nil {
		t.Fatal("取消後不能再配對")
	}
	// 沒帶標頭不能開配對（別的網站不能幫你開）
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/pairing", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("沒帶標頭：%d", rec.Code)
	}
}

func TestAddAndRemoveServer(t *testing.T) {
	s, h := newServer(t)
	redetected := 0
	s.Redetect = func() { redetected++ }
	hdr := map[string]string{"X-Local-AI": "1"}
	for _, bad := range []string{`{"url":""}`, `{"url":"ftp://x:21"}`, `{"url":"http://"}`, `壞掉`} {
		if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/servers", bad, hdr); rec.Code != http.StatusBadRequest {
			t.Errorf("%s 應該被拒絕：%d", bad, rec.Code)
		}
	}
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/servers", `{"url":"127.0.0.1:8080/v1/","name":"llama","apiKey":" k "}`, hdr); rec.Code != 200 {
		t.Fatalf("新增：%d", rec.Code)
	}
	servers := s.Store.Get().Servers
	if len(servers) != 1 || servers[0].URL != "http://127.0.0.1:8080" || servers[0].Name != "llama" || servers[0].APIKey != "k" || redetected != 1 {
		t.Fatalf("網址要整理好：%+v", servers)
	}
	// 狀態裡不能看到金鑰
	if rec := do(h, "GET", "127.0.0.1:11481", "/admin/api/status", "", nil); strings.Contains(rec.Body.String(), `"k"`) {
		t.Fatal("金鑰不能出現在狀態裡")
	}
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/servers/"+servers[0].ID+"/remove", "", hdr); rec.Code != 200 || len(s.Store.Get().Servers) != 0 {
		t.Fatalf("移除：%d", rec.Code)
	}
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/servers/nope/remove", "", hdr); rec.Code != http.StatusNotFound {
		t.Fatalf("移除不存在的：%d", rec.Code)
	}
}

func TestInstallOllama(t *testing.T) {
	s, h := newServer(t)
	m := ollama.NewManager(filepath.Join(t.TempDir(), "ollama"), "http://127.0.0.1:1", "")
	m.Installer.GOARCH = "386" // 不支援的電腦：馬上失敗，不會真的下載
	m.LookPath = func() string { return "" }
	s.Ollama = m
	up := false
	s.Engines = func(context.Context) []engine.Info { return []engine.Info{{Kind: engine.KindOllama, OK: up}} }
	post := func(path string) *httptest.ResponseRecorder {
		return do(h, "POST", "127.0.0.1:11481", path, "", map[string]string{"X-Local-AI": "1"})
	}
	var st Status
	_ = json.NewDecoder(do(h, "GET", "127.0.0.1:11481", "/admin/api/status", "", nil).Body).Decode(&st)
	if st.Ollama == nil || st.Ollama.Installed != "" {
		t.Fatalf("%+v", st.Ollama)
	}
	if rec := post("/admin/api/ollama/install"); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(3 * time.Second)
	for m.Installer.Progress().State != "failed" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p := m.Installer.Progress(); p.Code != "unsupported" {
		t.Fatalf("%+v", p)
	}
	// 使用者自己裝的 Ollama 不蓋掉；已經開著也不裝
	m.LookPath = func() string { return "/usr/bin/ollama" }
	if rec := post("/admin/api/ollama/install"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already_installed") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	up = true
	if rec := post("/admin/api/ollama/install"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already_running") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// 要帶 X-Local-AI（別的網站不能叫它裝東西）
	if rec := do(h, "POST", "127.0.0.1:11481", "/admin/api/ollama/install", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("%d", rec.Code)
	}
}
