package engine

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// CommonPorts OpenAI 格式伺服器常見的連接埠：llama.cpp／LocalAI 8080、vLLM 8000、Jan 1337、GPT4All 4891、text-generation-webui 5000
var CommonPorts = []int{8080, 8000, 1337, 4891, 5000}

// Server 使用者在管理頁自己加的 OpenAI 格式伺服器
type Server struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	APIKey string `json:"apiKey,omitempty"`
}

// Registry 找出這台電腦上的引擎、合併模型清單、聊天時找回模型是哪個引擎的。
// Ollama 不在這裡（gateway 直接轉送）；這裡管 LM Studio 與 OpenAI 格式的伺服器。
type Registry struct {
	LMStudio *LMStudio
	// Servers 使用者加的伺服器（每次都重新讀，管理頁改了馬上生效）
	Servers func() []Server
	// Reserved Ollama 已經用掉的模型名稱；其他引擎撞名時改叫「名稱@引擎」，Ollama 的模型名稱不變
	Reserved func(ctx context.Context) map[string]bool
	// DetectHost 自動偵測常見連接埠時用的主機（預設 127.0.0.1；測試可以換）
	DetectHost  string
	DetectPorts []int

	mu       sync.Mutex
	detected []*OpenAI
	detectAt time.Time
	routes   map[string]Backend
}

const detectEvery = 30 * time.Second

// Backends 目前所有的引擎（LM Studio、偵測到的、使用者加的）
func (r *Registry) Backends(ctx context.Context) []Backend {
	out := []Backend{}
	if r.LMStudio != nil {
		out = append(out, r.LMStudio)
	}
	for _, o := range r.detect(ctx) {
		out = append(out, o)
	}
	if r.Servers != nil {
		for _, s := range r.Servers() {
			label := s.Name
			if label == "" {
				label = hostPort(s.URL)
			}
			out = append(out, &OpenAI{EngineID: "server:" + s.ID, Label: label, BaseURL: strings.TrimRight(s.URL, "/"), APIKey: s.APIKey, Added: true})
		}
	}
	return out
}

// Infos 每個引擎的狀態；LM Studio 沒開就不列（偵測不到不代表有問題）
func (r *Registry) Infos(ctx context.Context) []Info {
	backends := r.Backends(ctx)
	infos := make([]Info, len(backends))
	var wg sync.WaitGroup
	for i, b := range backends {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infos[i] = b.Info(ctx)
		}()
	}
	wg.Wait()
	out := []Info{}
	for _, info := range infos {
		if info.OK || info.Added {
			out = append(out, info)
		}
	}
	return out
}

// Models 所有引擎的模型（同時問，連不到的略過）；跟 Ollama 或彼此撞名的，後面的名稱加上「@引擎」
func (r *Registry) Models(ctx context.Context) []Model {
	var taken map[string]bool
	if r.Reserved != nil {
		taken = r.Reserved(ctx)
	}
	backends := r.Backends(ctx)
	lists := make([][]Model, len(backends))
	var wg sync.WaitGroup
	for i, b := range backends {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if models, err := b.Models(c); err == nil {
				lists[i] = models
			}
		}()
	}
	wg.Wait()
	used := map[string]bool{}
	for k := range taken {
		used[k] = true
	}
	routes := map[string]Backend{}
	all := []Model{}
	for i, models := range lists {
		for _, m := range models {
			name := m.Name
			if used[name] {
				name = m.Name + "@" + backends[i].ID()
			}
			used[name] = true
			m.Name = name
			routes[name] = backends[i]
			all = append(all, m)
		}
	}
	r.mu.Lock()
	r.routes = routes
	r.mu.Unlock()
	return all
}

// Route 這個模型名稱是哪個引擎的（不是這裡的模型就回 nil，交給 Ollama）；回傳引擎認得的原本名稱
func (r *Registry) Route(ctx context.Context, name string) (Backend, string) {
	r.mu.Lock()
	b, ok := r.routes[name]
	r.mu.Unlock()
	if !ok {
		// 還沒列過模型（例如小幫手剛重開）：列一次再找
		r.Models(ctx)
		r.mu.Lock()
		b, ok = r.routes[name]
		r.mu.Unlock()
		if !ok {
			return nil, ""
		}
	}
	original := strings.TrimSuffix(name, "@"+b.ID())
	return b, original
}

// Find 找某個模型的資訊
func (r *Registry) Find(ctx context.Context, name string) (Model, bool) {
	for _, m := range r.Models(ctx) {
		if m.Name == name {
			return m, true
		}
	}
	return Model{}, false
}

// detect 在常見連接埠找 OpenAI 格式的伺服器（30 秒最多找一次）
func (r *Registry) detect(ctx context.Context) []*OpenAI {
	r.mu.Lock()
	if time.Since(r.detectAt) < detectEvery {
		out := r.detected
		r.mu.Unlock()
		return out
	}
	r.detectAt = time.Now()
	r.mu.Unlock()

	host := r.DetectHost
	if host == "" {
		host = "127.0.0.1"
	}
	ports := r.DetectPorts
	if ports == nil {
		ports = CommonPorts
	}
	added := map[string]bool{}
	if r.Servers != nil {
		for _, s := range r.Servers() {
			added[hostPort(s.URL)] = true
		}
	}
	found := make([]*OpenAI, len(ports))
	var wg sync.WaitGroup
	for i, port := range ports {
		base := fmt.Sprintf("http://%s:%d", host, port)
		if added[hostPort(base)] {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 認不出是哪一套時名稱留空，管理頁會寫成「OpenAI 相容伺服器」
			if name, ok := DetectName(ctx, base); ok {
				found[i] = &OpenAI{EngineID: fmt.Sprintf("detected:%d", port), Label: name, BaseURL: base}
			}
		}()
	}
	wg.Wait()
	out := []*OpenAI{}
	for _, o := range found {
		if o != nil {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EngineID < out[j].EngineID })
	r.mu.Lock()
	r.detected = out
	r.mu.Unlock()
	return out
}

// Redetect 下次馬上重新偵測（管理頁按「重新尋找」）
func (r *Registry) Redetect() {
	r.mu.Lock()
	r.detectAt = time.Time{}
	r.mu.Unlock()
}

func hostPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host
}
