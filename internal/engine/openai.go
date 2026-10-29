package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI 一個 OpenAI 格式的伺服器（llama.cpp、vLLM、Jan、GPT4All、LocalAI…）
type OpenAI struct {
	EngineID string
	Label    string
	BaseURL  string // 例如 http://127.0.0.1:8080（不含 /v1）
	APIKey   string
	Added    bool
}

func (o *OpenAI) ID() string { return o.EngineID }

func (o *OpenAI) Info(ctx context.Context) Info {
	info := Info{ID: o.EngineID, Kind: KindOpenAI, Name: o.Label, URL: o.BaseURL, Added: o.Added}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if models, err := o.Models(ctx); err == nil {
		info.OK, info.Models = true, len(models)
	}
	return info
}

func (o *OpenAI) Models(ctx context.Context) ([]Model, error) {
	var out struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := getJSON(ctx, o.BaseURL+"/v1/models", o.APIKey, &out); err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID == "" || looksLikeEmbedding(m.ID) {
			continue
		}
		models = append(models, Model{Name: m.ID, DisplayName: m.ID, Engine: o.EngineID, EngineName: o.Label})
	}
	return models, nil
}

func (o *OpenAI) Chat(ctx context.Context, req ChatRequest, w http.ResponseWriter) error {
	return openAIChat(ctx, o.BaseURL+"/v1/chat/completions", o.APIKey, req, "", w)
}

func (o *OpenAI) Load(context.Context, string) error   { return ErrUnsupported }
func (o *OpenAI) Unload(context.Context, string) error { return ErrUnsupported }

// DetectName 從 /v1/models 的 owned_by 猜是哪一套（llama.cpp、vLLM…），猜不到回空字串
func DetectName(ctx context.Context, baseURL string) (string, bool) {
	var out struct {
		Data []struct {
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
		Object string `json:"object"`
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if err := getJSON(ctx, baseURL+"/v1/models", "", &out); err != nil || out.Object != "list" {
		return "", false
	}
	owner := ""
	if len(out.Data) > 0 {
		owner = strings.ToLower(out.Data[0].OwnedBy)
	}
	switch {
	case strings.Contains(owner, "llamacpp"), strings.Contains(owner, "llama.cpp"):
		return "llama.cpp", true
	case strings.Contains(owner, "vllm"):
		return "vLLM", true
	case strings.Contains(owner, "jan"):
		return "Jan", true
	case strings.Contains(owner, "gpt4all"):
		return "GPT4All", true
	case strings.Contains(owner, "localai"):
		return "LocalAI", true
	}
	return "", true
}

func looksLikeEmbedding(id string) bool {
	id = strings.ToLower(id)
	return strings.Contains(id, "embed") || strings.Contains(id, "rerank")
}

// openAIChat 把 Ollama 的聊天請求翻成 OpenAI 格式送出，再把串流回答翻回 Ollama 的格式寫給手機。
// reasoning：LM Studio 的推理程度（空字串就不送）
func openAIChat(ctx context.Context, url, apiKey string, req ChatRequest, reasoning string, w http.ResponseWriter) error {
	body := map[string]any{
		"model":          req.Model,
		"messages":       toOpenAIMessages(req.Messages),
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if req.Options.NumPredict > 0 {
		body["max_tokens"] = req.Options.NumPredict
	}
	if req.Options.Temperature != nil {
		body["temperature"] = *req.Options.Temperature
	}
	if reasoning != "" {
		body["reasoning_effort"] = reasoning
	}
	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		return &Error{Status: http.StatusBadGateway, Message: "engine unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	send := func(v any) {
		_ = enc.Encode(v)
		if flusher != nil {
			flusher.Flush()
		}
	}

	var firstToken time.Time
	var usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	}
	finish := "stop"
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			send(map[string]any{"error": chunk.Error.Message})
			return nil
		}
		if chunk.Usage != nil {
			usage.PromptTokens, usage.CompletionTokens = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
		}
		for _, c := range chunk.Choices {
			thinking := c.Delta.ReasoningContent + c.Delta.Reasoning
			if c.Delta.Content != "" || thinking != "" {
				if firstToken.IsZero() {
					firstToken = time.Now()
				}
				msg := map[string]any{"role": "assistant", "content": c.Delta.Content}
				if thinking != "" {
					msg["thinking"] = thinking
				}
				send(map[string]any{"model": req.Model, "created_at": time.Now().UTC().Format(time.RFC3339Nano), "message": msg, "done": false})
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				finish = *c.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		send(map[string]any{"error": "stream interrupted: " + err.Error()})
		return nil
	}
	end := time.Now()
	evalStart := firstToken
	if evalStart.IsZero() {
		evalStart = end
	}
	send(map[string]any{
		"model":             req.Model,
		"created_at":        end.UTC().Format(time.RFC3339Nano),
		"message":           map[string]any{"role": "assistant", "content": ""},
		"done":              true,
		"done_reason":       finish,
		"total_duration":    end.Sub(start).Nanoseconds(),
		"load_duration":     0,
		"prompt_eval_count": usage.PromptTokens,
		"eval_count":        usage.CompletionTokens,
		"eval_duration":     end.Sub(evalStart).Nanoseconds(),
	})
	return nil
}

// toOpenAIMessages Ollama 的訊息（圖片是 base64 陣列）翻成 OpenAI 的格式（圖片是 image_url）
func toOpenAIMessages(msgs []ChatMessage) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if len(m.Images) == 0 {
			out = append(out, map[string]any{"role": m.Role, "content": m.Content})
			continue
		}
		parts := []map[string]any{}
		if m.Content != "" {
			parts = append(parts, map[string]any{"type": "text", "text": m.Content})
		}
		for _, img := range m.Images {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + imageType(img) + ";base64," + img}})
		}
		out = append(out, map[string]any{"role": m.Role, "content": parts})
	}
	return out
}

// imageType 從 base64 開頭猜圖片格式（手機送的是 JPEG）
func imageType(b64 string) string {
	switch {
	case strings.HasPrefix(b64, "iVBOR"):
		return "image/png"
	case strings.HasPrefix(b64, "UklGR"):
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func getJSON(ctx context.Context, url, apiKey string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return &Error{Status: http.StatusBadGateway, Message: "engine unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func postJSON(ctx context.Context, url, apiKey string, body, v any) error {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return &Error{Status: http.StatusBadGateway, Message: "engine unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}
	if v == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// readError 引擎的錯誤回應：OpenAI 是 {"error":{"message"}}，也有直接 {"error":"..."} 的
func readError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var e struct {
		Error any `json:"error"`
	}
	msg := strings.TrimSpace(string(data))
	if json.Unmarshal(data, &e) == nil {
		switch v := e.Error.(type) {
		case string:
			msg = v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				msg = m
			}
		}
	}
	if msg == "" {
		msg = fmt.Sprintf("engine returned HTTP %d", resp.StatusCode)
	}
	status := resp.StatusCode
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		// 引擎要金鑰：跟手機沒配對是兩回事，不能回 401
		status = http.StatusBadGateway
		msg = "the engine requires an API key: " + msg
	}
	return &Error{Status: status, Message: msg}
}
