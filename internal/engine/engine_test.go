package engine_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/yu-ken0207/local-ai-helper/internal/engine"
	"github.com/yu-ken0207/local-ai-helper/internal/engine/enginetest"
)

var ctx = context.Background()

func TestLMStudioModelsV1(t *testing.T) {
	fake := &enginetest.Fake{Models: []enginetest.FakeModel{
		{Key: "google/gemma-3-12b", Vision: true, Loaded: true},
		{Key: "openai/gpt-oss-20b", Reasoning: []string{"low", "medium", "high"}},
		{Key: "nomic-embed", Embedding: true},
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	l := &engine.LMStudio{BaseURL: srv.URL}
	models, err := l.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("嵌入模型不能聊天，要排除：%+v", models)
	}
	g := models[0]
	if g.Name != "google/gemma-3-12b" || !g.Vision || g.LoadedContext != 8192 || g.ContextLength != 32768 || g.Quantization != "Q4_K_M" || g.ParamSize != "12B" {
		t.Fatalf("gemma 的資訊不對：%+v", g)
	}
	if o := models[1]; o.Vision || o.LoadedContext != 0 || len(engine.ReasoningLevels(o.Reasoning)) != 3 {
		t.Fatalf("gpt-oss 的資訊不對：%+v", o)
	}
	if info := l.Info(ctx); !info.OK || info.Models != 2 || info.Kind != engine.KindLMStudio {
		t.Fatalf("狀態不對：%+v", info)
	}
}

func TestLMStudioOldVersion(t *testing.T) {
	fake := &enginetest.Fake{V0Only: true, Models: []enginetest.FakeModel{{Key: "qwen2-vl", Vision: true, Loaded: true}}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	l := &engine.LMStudio{BaseURL: srv.URL}
	models, err := l.Models(ctx)
	if err != nil || len(models) != 1 || !models[0].Vision || models[0].LoadedContext != 4096 {
		t.Fatalf("舊版 LM Studio：%v %+v", err, models)
	}
	if err := l.Load(ctx, "qwen2-vl"); err != engine.ErrUnsupported {
		t.Fatalf("舊版不能載入，要說做不到：%v", err)
	}
}

func TestLMStudioLoadUnload(t *testing.T) {
	fake := &enginetest.Fake{Models: []enginetest.FakeModel{{Key: "m"}}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	l := &engine.LMStudio{BaseURL: srv.URL}
	if err := l.Load(ctx, "m"); err != nil {
		t.Fatal(err)
	}
	if err := l.Unload(ctx, "m"); err != nil {
		t.Fatal(err)
	}
	if fake.Loads[0] != "m" || fake.Unloads[0] != "m" {
		t.Fatalf("載入 %v 卸載 %v", fake.Loads, fake.Unloads)
	}
}

// readNDJSON 讀出 Ollama 格式的每一行
func readNDJSON(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("不是 JSON：%q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

func TestChatTranslatesBothWays(t *testing.T) {
	fake := &enginetest.Fake{Models: []enginetest.FakeModel{{Key: "openai/gpt-oss-20b"}}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	l := &engine.LMStudio{BaseURL: srv.URL}
	req := engine.ChatRequest{Model: "openai/gpt-oss-20b", Think: "high", Messages: []engine.ChatMessage{
		{Role: "system", Content: "請用繁體中文回答。"},
		{Role: "user", Content: "這是什麼？", Images: []string{"/9j/AAAA", "iVBORw0KGgo"}},
	}}
	req.Options.NumPredict = 50
	rec := httptest.NewRecorder()
	if err := l.Chat(ctx, req, rec); err != nil {
		t.Fatal(err)
	}
	// 送出去的是 OpenAI 格式：圖片變 image_url、推理程度、串流、用量
	sent := fake.Requests[0]
	if sent["stream"] != true || sent["max_tokens"] != float64(50) || sent["reasoning_effort"] != "high" {
		t.Fatalf("請求不對：%v", sent)
	}
	parts := sent["messages"].([]any)[1].(map[string]any)["content"].([]any)
	if len(parts) != 3 || parts[0].(map[string]any)["text"] != "這是什麼？" ||
		!strings.HasPrefix(parts[1].(map[string]any)["image_url"].(map[string]any)["url"].(string), "data:image/jpeg;base64,/9j/") ||
		!strings.HasPrefix(parts[2].(map[string]any)["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,") {
		t.Fatalf("圖片沒翻好：%v", parts)
	}
	// 回來的是 Ollama 格式：思考、內容、最後一行統計
	lines := readNDJSON(t, rec.Body.String())
	if rec.Header().Get("Content-Type") != "application/x-ndjson" || len(lines) != 4 {
		t.Fatalf("回應：%s", rec.Body)
	}
	if lines[0]["message"].(map[string]any)["thinking"] != "想一下" || lines[1]["message"].(map[string]any)["content"] != "你" {
		t.Fatalf("思考或內容不對：%v", lines)
	}
	last := lines[3]
	if last["done"] != true || last["eval_count"] != float64(3) || last["prompt_eval_count"] != float64(12) || last["done_reason"] != "stop" {
		t.Fatalf("最後一行不對：%v", last)
	}
}

func TestToggleThinkIsNotSent(t *testing.T) {
	fake := &enginetest.Fake{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	l := &engine.LMStudio{BaseURL: srv.URL}
	_ = l.Chat(ctx, engine.ChatRequest{Model: "m", Think: true, Messages: []engine.ChatMessage{{Role: "user", Content: "hi"}}}, httptest.NewRecorder())
	if _, ok := fake.Requests[0]["reasoning_effort"]; ok {
		t.Fatal("開關型的思考不送 reasoning_effort（用模型預設）")
	}
}

func TestEngineNeedsKey(t *testing.T) {
	fake := &enginetest.Fake{APIKey: "secret", OpenAIOnly: true, Models: []enginetest.FakeModel{{Key: "m"}}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	o := &engine.OpenAI{EngineID: "x", BaseURL: srv.URL}
	_, err := o.Models(ctx)
	e, ok := err.(*engine.Error)
	if !ok || e.Status != http.StatusBadGateway || !strings.Contains(e.Message, "API key") {
		t.Fatalf("要金鑰時不能回 401（手機會以為沒配對）：%v", err)
	}
	o.APIKey = "secret"
	if models, err := o.Models(ctx); err != nil || len(models) != 1 {
		t.Fatalf("有金鑰就可以：%v", err)
	}
}

func port(t *testing.T, raw string) int {
	u, _ := url.Parse(raw)
	p, _ := strconv.Atoi(u.Port())
	return p
}

func TestRegistryDetectsAndRoutes(t *testing.T) {
	lm := httptest.NewServer(&enginetest.Fake{Models: []enginetest.FakeModel{{Key: "gemma4:12b"}, {Key: "google/gemma-3-12b"}}})
	defer lm.Close()
	llama := httptest.NewServer(&enginetest.Fake{OpenAIOnly: true, OwnedBy: "llamacpp", Models: []enginetest.FakeModel{{Key: "qwen.gguf"}, {Key: "bge-embed"}}})
	defer llama.Close()
	added := httptest.NewServer(&enginetest.Fake{OpenAIOnly: true, OwnedBy: "vllm", Models: []enginetest.FakeModel{{Key: "qwen.gguf"}}})
	defer added.Close()
	nothing := httptest.NewServer(http.NotFoundHandler()) // 常見連接埠上別的網站，不能當成引擎
	defer nothing.Close()

	r := &engine.Registry{
		LMStudio:    &engine.LMStudio{BaseURL: lm.URL},
		DetectPorts: []int{port(t, llama.URL), port(t, nothing.URL)},
		Servers:     func() []engine.Server { return []engine.Server{{ID: "a1", Name: "實驗室", URL: added.URL}} },
	}
	infos := r.Infos(ctx)
	names := []string{}
	for _, i := range infos {
		names = append(names, i.Name)
	}
	if strings.Join(names, ",") != "LM Studio,llama.cpp,實驗室" {
		t.Fatalf("引擎：%v", names)
	}

	// Ollama 已經有 gemma4:12b：LM Studio 的同名模型加上 @lmstudio；llama.cpp 與手動加的伺服器都有 qwen.gguf
	r.Reserved = func(context.Context) map[string]bool { return map[string]bool{"gemma4:12b": true} }
	models := r.Models(ctx)
	got := []string{}
	for _, m := range models {
		got = append(got, m.Name)
	}
	if strings.Join(got, ",") != "gemma4:12b@lmstudio,google/gemma-3-12b,qwen.gguf,qwen.gguf@server:a1" {
		t.Fatalf("模型：%v", got)
	}
	// 路由表重建也一樣讓 Ollama 優先：同名的不能被 LM Studio 搶走
	r.Models(ctx)
	if b, _ := r.Route(ctx, "gemma4:12b"); b != nil {
		t.Fatal("gemma4:12b 是 Ollama 的")
	}
	b, original := r.Route(ctx, "gemma4:12b@lmstudio")
	if b == nil || b.ID() != engine.KindLMStudio || original != "gemma4:12b" {
		t.Fatalf("撞名的模型要找回原本的名稱：%v %q", b, original)
	}
	if b, original := r.Route(ctx, "qwen.gguf@server:a1"); b == nil || b.ID() != "server:a1" || original != "qwen.gguf" {
		t.Fatalf("手動加的伺服器：%v %q", b, original)
	}
	if b, _ := r.Route(ctx, "llama3:latest"); b != nil {
		t.Fatal("不是這裡的模型要交給 Ollama")
	}
}
