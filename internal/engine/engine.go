// Package engine 接 Ollama 以外的 AI 引擎（LM Studio、OpenAI 格式的伺服器），把它們翻成 Ollama 的樣子給手機。
// Ollama 本身由 gateway 直接轉送，不經過這裡。
package engine

import (
	"context"
	"net/http"
	"time"
)

// Kind 引擎種類
const (
	KindOllama   = "ollama"
	KindLMStudio = "lmstudio"
	KindOpenAI   = "openai"
)

// Model 一個可以聊天的模型（照 Ollama /api/tags、/api/show 需要的資訊）
type Model struct {
	// Name 給手機的名稱（聊天時用它找回是哪個引擎）
	Name        string
	DisplayName string
	Engine      string // 引擎 ID
	Vision      bool
	// Reasoning 可以選的推理程度（LM Studio：off/on/low/medium/high）；空的就是不能調
	Reasoning     []string
	ContextLength int
	Family        string
	ParamSize     string
	Quantization  string
	SizeBytes     int64
	// LoadedContext 已經載入時的上下文長度；0 代表沒載入
	LoadedContext int
}

// Info 引擎狀態（管理頁、手機的 /helper/v1/info 都用）
type Info struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	OK      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Models  int    `json:"models"`
	// Added 使用者在管理頁自己加的（可以移除）
	Added bool `json:"added,omitempty"`
}

// Backend Ollama 以外的引擎
type Backend interface {
	ID() string
	Info(ctx context.Context) Info
	Models(ctx context.Context) ([]Model, error)
	// Chat 照 Ollama /api/chat 的請求聊天，把回答用 Ollama 的格式（一行一個 JSON）寫回去
	Chat(ctx context.Context, req ChatRequest, w http.ResponseWriter) error
	// Load／Unload 預先載入、卸載；做不到回 ErrUnsupported
	Load(ctx context.Context, model string) error
	Unload(ctx context.Context, model string) error
}

// ChatRequest Ollama /api/chat 的請求（手機送來的）
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   *bool         `json:"stream,omitempty"`
	Think    any           `json:"think,omitempty"`
	Options  struct {
		NumPredict  int      `json:"num_predict,omitempty"`
		Temperature *float64 `json:"temperature,omitempty"`
	} `json:"options"`
}

type ChatMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

// Error 引擎回報的錯誤（照 HTTP 狀態碼回給手機）
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// ErrUnsupported 這個引擎做不到
var ErrUnsupported = &Error{Status: http.StatusNotImplemented, Message: "not supported by this engine"}

var client = &http.Client{Timeout: 0}

// probeTimeout 偵測引擎在不在的逾時
const probeTimeout = 1500 * time.Millisecond
