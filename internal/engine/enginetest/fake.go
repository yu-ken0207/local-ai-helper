// Package enginetest 測試用的假引擎：假 LM Studio（原生 API＋OpenAI 格式）、假 OpenAI 格式伺服器。
package enginetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// FakeModel 假 LM Studio 的模型
type FakeModel struct {
	Key       string
	Vision    bool
	Tools     bool
	Reasoning []string
	Loaded    bool
	Embedding bool
}

// Fake 假的 LM Studio 或 OpenAI 格式伺服器
type Fake struct {
	mu sync.Mutex
	// V0Only 模擬舊版 LM Studio（沒有 /api/v1）
	V0Only bool
	// OpenAIOnly 只有 /v1（llama.cpp、vLLM 那種）
	OpenAIOnly bool
	OwnedBy    string
	Models     []FakeModel
	// APIKey 要金鑰時填
	APIKey string
	// Requests 收到的聊天請求內容
	Requests []map[string]any
	Loads    []string
	Unloads  []string
	// Downloads 叫它下載的模型；下載進度每問一次多 1000 bytes，總共 3000
	Downloads []string
	polls     int
}

func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.APIKey != "" && r.Header.Get("Authorization") != "Bearer "+f.APIKey {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"Invalid API key"}}`)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v1/models" && !f.V0Only && !f.OpenAIOnly:
		models := []map[string]any{}
		for _, m := range f.Models {
			typ := "llm"
			if m.Embedding {
				typ = "embedding"
			}
			item := map[string]any{"type": typ, "key": m.Key, "display_name": "Display " + m.Key, "architecture": "gemma3", "quantization": map[string]any{"name": "Q4_K_M"}, "size_bytes": 1000, "params_string": "12B", "max_context_length": 32768, "loaded_instances": []any{}}
			if m.Loaded {
				item["loaded_instances"] = []any{map[string]any{"id": m.Key, "config": map[string]any{"context_length": 8192}}}
			}
			caps := map[string]any{"vision": m.Vision, "trained_for_tool_use": m.Tools}
			if len(m.Reasoning) > 0 {
				caps["reasoning"] = map[string]any{"allowed_options": m.Reasoning, "default": m.Reasoning[0]}
			}
			item["capabilities"] = caps
			models = append(models, item)
		}
		writeJSON(w, map[string]any{"models": models})
	case r.URL.Path == "/api/v0/models" && !f.OpenAIOnly:
		data := []map[string]any{}
		for _, m := range f.Models {
			typ := "llm"
			if m.Vision {
				typ = "vlm"
			}
			state := "not-loaded"
			if m.Loaded {
				state = "loaded"
			}
			data = append(data, map[string]any{"id": m.Key, "type": typ, "arch": "qwen2", "quantization": "Q4", "state": state, "max_context_length": 4096})
		}
		writeJSON(w, map[string]any{"data": data})
	case r.URL.Path == "/api/v1/models/load" && !f.V0Only && !f.OpenAIOnly:
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.Loads = append(f.Loads, fmt.Sprint(b["model"]))
		writeJSON(w, map[string]any{"type": "llm", "instance_id": b["model"], "status": "loaded"})
	case r.URL.Path == "/api/v1/models/unload" && !f.V0Only && !f.OpenAIOnly:
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.Unloads = append(f.Unloads, fmt.Sprint(b["instance_id"]))
		writeJSON(w, map[string]any{"instance_id": b["instance_id"]})
	case r.URL.Path == "/api/v1/models/download" && !f.V0Only && !f.OpenAIOnly:
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		model := fmt.Sprint(b["model"])
		for _, m := range f.Models {
			if m.Key == model {
				writeJSON(w, map[string]any{"status": "already_downloaded"})
				return
			}
		}
		f.Downloads = append(f.Downloads, model)
		writeJSON(w, map[string]any{"job_id": "job_1", "status": "downloading", "total_size_bytes": 3000})
	case r.URL.Path == "/api/v1/models/download/status/job_1":
		f.polls++
		done := int64(f.polls * 1000)
		status := "downloading"
		if done >= 3000 {
			done, status = 3000, "completed"
		}
		writeJSON(w, map[string]any{"job_id": "job_1", "status": status, "total_size_bytes": 3000, "downloaded_bytes": done})
	case r.URL.Path == "/v1/models":
		data := []map[string]any{}
		for _, m := range f.Models {
			data = append(data, map[string]any{"id": m.Key, "object": "model", "owned_by": f.OwnedBy})
		}
		writeJSON(w, map[string]any{"object": "list", "data": data})
	case r.URL.Path == "/v1/chat/completions":
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.Requests = append(f.Requests, b)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		send := func(s string) {
			fmt.Fprintf(w, "data: %s\n\n", s)
			flusher.Flush()
		}
		model := fmt.Sprint(b["model"])
		if _, ok := b["tools"]; ok {
			// 有給工具：照 OpenAI 的樣子一段一段送兩個工具呼叫（arguments 拆開送）
			send(`{"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"create_event","arguments":""}}]}}]}`)
			send(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"title\":\"看牙醫\","}}]}}]}`)
			send(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"start\":\"2026-11-05T15:00\"}"}}]}}]}`)
			send(`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"list_events","arguments":"{bad"}}]},"finish_reason":"tool_calls"}]}`)
			send(`{"choices":[],"usage":{"prompt_tokens":40,"completion_tokens":9}}`)
			send(`[DONE]`)
			return
		}
		send(`{"choices":[{"delta":{"role":"assistant","reasoning_content":"想一下"}}]}`)
		send(`{"choices":[{"delta":{"content":"你"}}]}`)
		send(`{"choices":[{"delta":{"content":"好 ` + model + `"},"finish_reason":"stop"}]}`)
		send(`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`)
		send(`[DONE]`)
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"Unexpected endpoint or method."}`)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
