package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
	"github.com/yu-ken0207/local-ai-helper/internal/netinfo"
)

func newServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store, Version: "test", Addrs: func() []netinfo.Addr { return []netinfo.Addr{{IP: "192.168.1.20", Kind: "lan"}} }}
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
	if st.App != AppID || st.Version != "test" || st.APIPort != config.DefaultAPIPort || len(st.Addrs) != 1 {
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
