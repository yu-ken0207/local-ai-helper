package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
	"github.com/yu-ken0207/local-ai-helper/internal/engine"
	"github.com/yu-ken0207/local-ai-helper/internal/engine/enginetest"
	"github.com/yu-ken0207/local-ai-helper/internal/pairing"
)

// fakeOllama 假的 Ollama：記下收到的請求，/api/chat 一行一行慢慢送
type fakeOllama struct {
	mu       sync.Mutex
	requests []*http.Request
	auth     []string
}

func (f *fakeOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()
	switch r.URL.Path {
	case "/api/version":
		fmt.Fprint(w, `{"version":"0.40.0"}`)
	case "/api/tags":
		fmt.Fprint(w, `{"models":[{"name":"gemma4:12b"}]}`)
	case "/api/chat":
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, part := range []string{"你", "好"} {
			fmt.Fprintf(w, `{"message":{"role":"assistant","content":%q},"done":false}`+"\n", part)
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
		fmt.Fprint(w, `{"done":true}`+"\n")
	default:
		http.NotFound(w, r)
	}
}

func setup(t *testing.T, ollamaURL string) (*Server, http.Handler) {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store, Pairing: pairing.New(store), Version: "test", OllamaURL: ollamaURL}
	return s, s.Handler()
}

func call(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func pairPhone(t *testing.T, s *Server, h http.Handler) string {
	t.Helper()
	code := s.Pairing.Start().Code
	rec := call(h, "POST", "/helper/v1/pair", "", `{"code":"`+code+`","deviceName":"測試手機"}`)
	if rec.Code != 200 {
		t.Fatalf("配對 %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Token  string            `json:"token"`
		Helper map[string]string `json:"helper"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out.Token == "" || out.Helper["id"] != s.Store.Get().ID {
		t.Fatalf("配對回應不對：%+v", out)
	}
	return out.Token
}

func TestHelloNeedsNoToken(t *testing.T) {
	s, h := setup(t, "http://127.0.0.1:1")
	rec := call(h, "GET", "/helper/v1/hello", "", "")
	var hello Hello
	_ = json.NewDecoder(rec.Body).Decode(&hello)
	if rec.Code != 200 || hello.App != AppID || hello.ID != s.Store.Get().ID || hello.Pairing {
		t.Fatalf("hello 不對：%d %+v", rec.Code, hello)
	}
	s.Pairing.Start()
	rec = call(h, "GET", "/helper/v1/hello", "", "")
	_ = json.NewDecoder(rec.Body).Decode(&hello)
	if !hello.Pairing {
		t.Fatal("配對中要告訴 App")
	}
}

func TestPairErrors(t *testing.T) {
	s, h := setup(t, "http://127.0.0.1:1")
	if rec := call(h, "POST", "/helper/v1/pair", "", `{"code":"123456"}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not_pairing") {
		t.Fatalf("沒開配對：%d %s", rec.Code, rec.Body)
	}
	code := s.Pairing.Start().Code
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if rec := call(h, "POST", "/helper/v1/pair", "", `{"code":"`+wrong+`"}`); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "bad_code") {
		t.Fatalf("打錯碼：%d %s", rec.Code, rec.Body)
	}
	if rec := call(h, "POST", "/helper/v1/pair", "", `壞掉`); rec.Code != http.StatusBadRequest {
		t.Fatalf("壞掉的內容：%d", rec.Code)
	}
}

func TestAPINeedsToken(t *testing.T) {
	f := &fakeOllama{}
	ollama := httptest.NewServer(f)
	defer ollama.Close()
	_, h := setup(t, ollama.URL)
	for _, path := range []string{"/api/tags", "/helper/v1/info"} {
		rec := call(h, "GET", path, "", "")
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "not_paired") {
			t.Errorf("%s 沒權杖應該 401：%d", path, rec.Code)
		}
		if rec := call(h, "GET", path, "亂猜", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s 錯的權杖應該 401：%d", path, rec.Code)
		}
	}
	if len(f.requests) != 0 {
		t.Fatal("沒配對的請求不能送到 Ollama")
	}
}

func TestProxyToOllamaAndStripToken(t *testing.T) {
	f := &fakeOllama{}
	ollama := httptest.NewServer(f)
	defer ollama.Close()
	s, h := setup(t, ollama.URL)
	token := pairPhone(t, s, h)

	rec := call(h, "GET", "/api/tags", token, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "gemma4:12b") {
		t.Fatalf("模型清單：%d %s", rec.Code, rec.Body)
	}
	if f.auth[0] != "" {
		t.Fatal("權杖不能送到 Ollama")
	}
	// 不開放的 API 不轉
	for _, path := range []string{"/api/delete", "/api/create", "/api/push", "/api/copy"} {
		if rec := call(h, "POST", path, token, "{}"); rec.Code != http.StatusNotFound {
			t.Errorf("%s 應該不轉：%d", path, rec.Code)
		}
	}
	if len(f.requests) != 1 {
		t.Fatalf("不開放的 API 不能送到 Ollama：%d", len(f.requests))
	}
}

func TestChatStreamsThroughHelper(t *testing.T) {
	ollama := httptest.NewServer(&fakeOllama{})
	defer ollama.Close()
	s, h := setup(t, ollama.URL)
	srv := httptest.NewServer(h)
	defer srv.Close()
	token := pairPhone(t, s, h)

	req, _ := http.NewRequest("POST", srv.URL+"/api/chat", strings.NewReader(`{"model":"gemma4:12b","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	first, err := r.ReadString('\n')
	if err != nil || !strings.Contains(first, "你") {
		t.Fatalf("第一行：%q %v", first, err)
	}
	// 串流：第一個字要在全部送完之前就到（假 Ollama 要 100ms 以上才送完）
	if elapsed := time.Since(start); elapsed > 90*time.Millisecond {
		t.Fatalf("第一個字 %v 才到，沒有邊收邊送", elapsed)
	}
	rest, _ := io.ReadAll(r)
	if !strings.Contains(string(rest), "好") || !strings.Contains(string(rest), `"done":true`) {
		t.Fatalf("後面的內容：%s", rest)
	}
}

func TestInfoAndOllamaDown(t *testing.T) {
	ollama := httptest.NewServer(&fakeOllama{})
	s, h := setup(t, ollama.URL)
	token := pairPhone(t, s, h)
	rec := call(h, "GET", "/helper/v1/info", token, "")
	var info Info
	_ = json.NewDecoder(rec.Body).Decode(&info)
	if info.Device != "測試手機" || len(info.Engines) != 1 || !info.Engines[0].OK || info.Engines[0].Version != "0.40.0" || !info.Features["pull"] {
		t.Fatalf("info 不對：%+v", info)
	}
	// Ollama 關掉：info 照樣回答，標成不能用；轉送回 502 並說明
	ollama.Close()
	rec = call(h, "GET", "/helper/v1/info", token, "")
	_ = json.NewDecoder(rec.Body).Decode(&info)
	if info.Engines[0].OK || info.Features["pull"] {
		t.Fatalf("Ollama 關掉要標成不能用：%+v", info)
	}
	// 一個引擎都沒有：模型清單說清楚；聊天轉給 Ollama 失敗也說清楚
	if rec := call(h, "GET", "/api/tags", token, ""); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "no_engine") {
		t.Fatalf("沒有引擎時：%d %s", rec.Code, rec.Body)
	}
	if rec := call(h, "POST", "/api/chat", token, `{"model":"gemma4:12b"}`); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "engine_unreachable") {
		t.Fatalf("Ollama 關掉時聊天：%d %s", rec.Code, rec.Body)
	}
}

// 有 LM Studio 時：模型清單合在一起，聊天依模型轉給對的引擎
func setupWithLMStudio(t *testing.T, ollamaURL string) (*Server, http.Handler, *enginetest.Fake, string) {
	t.Helper()
	lm := &enginetest.Fake{Models: []enginetest.FakeModel{
		{Key: "google/gemma-3-12b", Vision: true, Loaded: true},
		{Key: "openai/gpt-oss-20b", Reasoning: []string{"low", "medium", "high"}},
		{Key: "gemma4:12b"},
	}}
	lmSrv := httptest.NewServer(lm)
	t.Cleanup(lmSrv.Close)
	s, _ := setup(t, ollamaURL)
	s.Registry = &engine.Registry{LMStudio: &engine.LMStudio{BaseURL: lmSrv.URL}, DetectPorts: []int{}}
	h := s.Handler()
	return s, h, lm, pairPhone(t, s, h)
}

func TestTagsMergeEngines(t *testing.T) {
	ollama := httptest.NewServer(&fakeOllama{})
	defer ollama.Close()
	_, h, _, token := setupWithLMStudio(t, ollama.URL)
	rec := call(h, "GET", "/api/tags", token, "")
	var out struct {
		Models []struct {
			Name   string `json:"name"`
			Engine string `json:"engine"`
		} `json:"models"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&out)
	got := []string{}
	for _, m := range out.Models {
		got = append(got, m.Name+"|"+m.Engine)
	}
	// Ollama 也有 gemma4:12b：LM Studio 那個加上 @lmstudio
	if want := "gemma4:12b|ollama,google/gemma-3-12b|lmstudio,openai/gpt-oss-20b|lmstudio,gemma4:12b@lmstudio|lmstudio"; strings.Join(got, ",") != want {
		t.Fatalf("模型清單 %v", got)
	}
}

func TestShowForLMStudioModel(t *testing.T) {
	ollama := httptest.NewServer(&fakeOllama{})
	defer ollama.Close()
	_, h, _, token := setupWithLMStudio(t, ollama.URL)
	var show struct {
		Capabilities []string       `json:"capabilities"`
		ModelInfo    map[string]any `json:"model_info"`
		Thinking     struct {
			Values []string `json:"values"`
		} `json:"thinking"`
	}
	_ = json.NewDecoder(call(h, "POST", "/api/show", token, `{"model":"google/gemma-3-12b"}`).Body).Decode(&show)
	if strings.Join(show.Capabilities, ",") != "completion,vision" || show.ModelInfo["lmstudio.context_length"] != float64(32768) {
		t.Fatalf("看圖模型：%+v", show)
	}
	_ = json.NewDecoder(call(h, "POST", "/api/show", token, `{"model":"openai/gpt-oss-20b"}`).Body).Decode(&show)
	if !strings.Contains(strings.Join(show.Capabilities, ","), "thinking") || strings.Join(show.Thinking.Values, ",") != "low,medium,high" {
		t.Fatalf("推理模型：%+v", show)
	}
}

func TestChatRoutesToLMStudio(t *testing.T) {
	ollama := httptest.NewServer(&fakeOllama{})
	defer ollama.Close()
	_, h, lm, token := setupWithLMStudio(t, ollama.URL)
	rec := call(h, "POST", "/api/chat", token, `{"model":"gemma4:12b@lmstudio","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != 200 || len(lm.Requests) != 1 || lm.Requests[0]["model"] != "gemma4:12b" {
		t.Fatalf("要用原本的名稱轉給 LM Studio：%d %v", rec.Code, lm.Requests)
	}
	// 回答裡的模型名稱換回手機看到的名稱
	if !strings.Contains(rec.Body.String(), `"model":"gemma4:12b@lmstudio"`) || !strings.Contains(rec.Body.String(), `"done":true`) {
		t.Fatalf("回答：%s", rec.Body)
	}
	// Ollama 的模型還是給 Ollama
	if rec := call(h, "POST", "/api/chat", token, `{"model":"gemma4:12b","messages":[]}`); !strings.Contains(rec.Body.String(), "你") || len(lm.Requests) != 1 {
		t.Fatalf("Ollama 的模型：%s", rec.Body)
	}
}

func TestPreloadUnloadAndPs(t *testing.T) {
	ollama := httptest.NewServer(&fakeOllama{})
	defer ollama.Close()
	_, h, lm, token := setupWithLMStudio(t, ollama.URL)
	if rec := call(h, "POST", "/api/generate", token, `{"model":"openai/gpt-oss-20b","keep_alive":"30m"}`); rec.Code != 200 {
		t.Fatalf("預先載入：%d %s", rec.Code, rec.Body)
	}
	if rec := call(h, "POST", "/api/generate", token, `{"model":"google/gemma-3-12b","keep_alive":0}`); rec.Code != 200 {
		t.Fatalf("卸載：%d %s", rec.Code, rec.Body)
	}
	if strings.Join(lm.Loads, ",") != "openai/gpt-oss-20b" || strings.Join(lm.Unloads, ",") != "google/gemma-3-12b" {
		t.Fatalf("載入 %v 卸載 %v", lm.Loads, lm.Unloads)
	}
	var ps struct {
		Models []struct {
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
		} `json:"models"`
	}
	_ = json.NewDecoder(call(h, "GET", "/api/ps", token, "").Body).Decode(&ps)
	if len(ps.Models) != 1 || ps.Models[0].Name != "google/gemma-3-12b" || ps.Models[0].ContextLength != 8192 {
		t.Fatalf("已載入：%+v", ps.Models)
	}
}

func TestWorksWithoutOllama(t *testing.T) {
	s, h, _, token := setupWithLMStudio(t, "http://127.0.0.1:1")
	if rec := call(h, "GET", "/api/tags", token, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "google/gemma-3-12b") {
		t.Fatalf("沒有 Ollama 也要列 LM Studio 的模型：%d %s", rec.Code, rec.Body)
	}
	if rec := call(h, "GET", "/api/version", token, ""); !strings.Contains(rec.Body.String(), `"version":"test"`) {
		t.Fatalf("沒有 Ollama 時回小幫手的版本：%s", rec.Body)
	}
	var info Info
	_ = json.NewDecoder(call(h, "GET", "/helper/v1/info", token, "").Body).Decode(&info)
	if len(info.Engines) != 2 || info.Engines[0].OK || !info.Engines[1].OK || info.Features["pull"] || !info.Features["preload"] {
		t.Fatalf("info：%+v", info)
	}
	_ = s
}
