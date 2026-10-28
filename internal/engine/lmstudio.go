package engine

import (
	"context"
	"net/http"
	"strings"
)

// LMStudio LM Studio 的伺服器（預設 http://127.0.0.1:1234）。
// 模型清單用原生 API（0.4 起 /api/v1/models，舊版 /api/v0/models），聊天用 OpenAI 格式。
type LMStudio struct {
	BaseURL string
	APIKey  string
}

func (l *LMStudio) ID() string { return KindLMStudio }

func (l *LMStudio) Info(ctx context.Context) Info {
	info := Info{ID: KindLMStudio, Kind: KindLMStudio, Name: "LM Studio", URL: l.BaseURL}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if models, err := l.Models(ctx); err == nil {
		info.OK, info.Models = true, len(models)
	}
	return info
}

func (l *LMStudio) Models(ctx context.Context) ([]Model, error) {
	models, err := l.modelsV1(ctx)
	if e, ok := err.(*Error); ok && e.Status == http.StatusNotFound {
		return l.modelsV0(ctx)
	}
	return models, err
}

// modelsV1 LM Studio 0.4 起的 /api/v1/models
func (l *LMStudio) modelsV1(ctx context.Context) ([]Model, error) {
	var out struct {
		Models []struct {
			Type         string `json:"type"`
			Key          string `json:"key"`
			DisplayName  string `json:"display_name"`
			Architecture string `json:"architecture"`
			Quantization *struct {
				Name string `json:"name"`
			} `json:"quantization"`
			SizeBytes       int64  `json:"size_bytes"`
			ParamsString    string `json:"params_string"`
			MaxContext      int    `json:"max_context_length"`
			LoadedInstances []struct {
				Config struct {
					ContextLength int `json:"context_length"`
				} `json:"config"`
			} `json:"loaded_instances"`
			Capabilities *struct {
				Vision    bool `json:"vision"`
				Reasoning *struct {
					AllowedOptions []string `json:"allowed_options"`
				} `json:"reasoning"`
			} `json:"capabilities"`
		} `json:"models"`
	}
	if err := getJSON(ctx, l.BaseURL+"/api/v1/models", l.APIKey, &out); err != nil {
		return nil, err
	}
	models := []Model{}
	for _, m := range out.Models {
		if m.Type != "llm" || m.Key == "" {
			continue
		}
		model := Model{Name: m.Key, DisplayName: m.DisplayName, Engine: KindLMStudio, ContextLength: m.MaxContext, Family: m.Architecture, ParamSize: m.ParamsString, SizeBytes: m.SizeBytes}
		if m.Quantization != nil {
			model.Quantization = m.Quantization.Name
		}
		if m.Capabilities != nil {
			model.Vision = m.Capabilities.Vision
			if m.Capabilities.Reasoning != nil {
				model.Reasoning = m.Capabilities.Reasoning.AllowedOptions
			}
		}
		if len(m.LoadedInstances) > 0 {
			model.LoadedContext = m.LoadedInstances[0].Config.ContextLength
			if model.LoadedContext == 0 {
				model.LoadedContext = m.MaxContext
			}
		}
		models = append(models, model)
	}
	return models, nil
}

// modelsV0 舊版 LM Studio（0.3）的 /api/v0/models
func (l *LMStudio) modelsV0(ctx context.Context) ([]Model, error) {
	var out struct {
		Data []struct {
			ID           string `json:"id"`
			Type         string `json:"type"`
			Arch         string `json:"arch"`
			Quantization string `json:"quantization"`
			State        string `json:"state"`
			MaxContext   int    `json:"max_context_length"`
		} `json:"data"`
	}
	if err := getJSON(ctx, l.BaseURL+"/api/v0/models", l.APIKey, &out); err != nil {
		return nil, err
	}
	models := []Model{}
	for _, m := range out.Data {
		if (m.Type != "llm" && m.Type != "vlm") || m.ID == "" {
			continue
		}
		model := Model{Name: m.ID, DisplayName: m.ID, Engine: KindLMStudio, Vision: m.Type == "vlm", ContextLength: m.MaxContext, Family: m.Arch, Quantization: m.Quantization}
		if m.State == "loaded" {
			model.LoadedContext = m.MaxContext
		}
		models = append(models, model)
	}
	return models, nil
}

func (l *LMStudio) Chat(ctx context.Context, req ChatRequest, w http.ResponseWriter) error {
	return openAIChat(ctx, l.BaseURL+"/v1/chat/completions", l.APIKey, req, reasoningEffort(req.Think), w)
}

// reasoningEffort 手機選的推理程度（low／medium／high）；開關型不送，用模型預設
func reasoningEffort(think any) string {
	if v, ok := think.(string); ok {
		switch v = strings.ToLower(v); v {
		case "low", "medium", "high":
			return v
		}
	}
	return ""
}

// ReasoningLevels 手機可以選的推理程度（只算 low／medium／high；只有開關的模型不給選）
func ReasoningLevels(options []string) []string {
	levels := []string{}
	for _, o := range options {
		switch o {
		case "low", "medium", "high":
			levels = append(levels, o)
		}
	}
	return levels
}

func (l *LMStudio) Load(ctx context.Context, model string) error {
	err := postJSON(ctx, l.BaseURL+"/api/v1/models/load", l.APIKey, map[string]any{"model": model}, nil)
	return notFoundUnsupported(err)
}

func (l *LMStudio) Unload(ctx context.Context, model string) error {
	err := postJSON(ctx, l.BaseURL+"/api/v1/models/unload", l.APIKey, map[string]any{"instance_id": model}, nil)
	return notFoundUnsupported(err)
}

// 舊版 LM Studio 沒有載入／卸載的 API
func notFoundUnsupported(err error) error {
	if e, ok := err.(*Error); ok && e.Status == http.StatusNotFound {
		return ErrUnsupported
	}
	return err
}
