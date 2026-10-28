// Package config 存小幫手的設定：識別碼、電腦名稱、連接埠、已配對的手機。
// 設定檔是 JSON，放在使用者的設定資料夾（Windows：%AppData%\local-ai-helper；Linux：~/.config/local-ai-helper）。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultAPIPort 手機連的連接埠（聽區網）
	DefaultAPIPort = 11480
	// DefaultAdminPort 本機管理頁的連接埠（只聽 127.0.0.1）
	DefaultAdminPort = 11481
	// DefaultOllamaURL Ollama 預設的位址
	DefaultOllamaURL = "http://127.0.0.1:11434"
	// DefaultLMStudioURL LM Studio 伺服器預設的位址
	DefaultLMStudioURL = "http://127.0.0.1:1234"
	fileName           = "config.json"
)

// Device 一支配對過的手機。權杖只存雜湊，設定檔外洩也拿不到權杖本身。
type Device struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"tokenHash"`
	PairedAt   time.Time  `json:"pairedAt"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

type Config struct {
	// ID 這台小幫手的識別碼；手機用它認出同一台電腦（位址會變）
	ID string `json:"id"`
	// Name 顯示在手機上的電腦名稱
	Name      string `json:"name"`
	APIPort   int    `json:"apiPort"`
	AdminPort int    `json:"adminPort"`
	// OllamaURL 這台電腦上的 Ollama（小幫手從本機連，Ollama 不用打開網路分享）
	OllamaURL string `json:"ollamaUrl"`
	// LMStudioURL LM Studio 的伺服器；LMStudioToken 有開「需要 API 金鑰」時填
	LMStudioURL   string `json:"lmstudioUrl"`
	LMStudioToken string `json:"lmstudioToken,omitempty"`
	// Servers 使用者自己加的 OpenAI 格式伺服器
	Servers []Server `json:"servers"`
	Devices []Device `json:"devices"`
}

// Server 使用者在管理頁加的 OpenAI 格式伺服器（llama.cpp、vLLM…）
type Server struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	APIKey string `json:"apiKey,omitempty"`
}

// Dir 設定資料夾
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "local-ai-helper"), nil
}

// NewID 隨機識別碼（十六進位）
func NewID(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func defaultName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "Computer"
}

// fillDefaults 補上沒填的欄位（第一次執行、或舊版設定檔少了新欄位）
func (c *Config) fillDefaults() {
	if c.ID == "" {
		c.ID = NewID(16)
	}
	if c.Name == "" {
		c.Name = defaultName()
	}
	if c.APIPort == 0 {
		c.APIPort = DefaultAPIPort
	}
	if c.AdminPort == 0 {
		c.AdminPort = DefaultAdminPort
	}
	if c.OllamaURL == "" {
		c.OllamaURL = DefaultOllamaURL
	}
	if c.LMStudioURL == "" {
		c.LMStudioURL = DefaultLMStudioURL
	}
	if c.Servers == nil {
		c.Servers = []Server{}
	}
	if c.Devices == nil {
		c.Devices = []Device{}
	}
}

// Store 設定加鎖，改完立刻寫回檔案
type Store struct {
	mu  sync.Mutex
	dir string
	cfg Config
}

// Open 讀設定；沒有設定檔就建一份預設的並存起來
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, fileName))
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &s.cfg); err != nil {
			return nil, err
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, err
	}
	s.cfg.fillDefaults()
	if err := s.save(); err != nil {
		return nil, err
	}
	return s, nil
}

// Get 目前設定的複本
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	c.Devices = append([]Device{}, s.cfg.Devices...)
	c.Servers = append([]Server{}, s.cfg.Servers...)
	return c
}

// Update 修改設定並存檔；存檔失敗就不改
func (s *Store) Update(fn func(c *Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.cfg
	before.Devices = append([]Device(nil), s.cfg.Devices...)
	before.Servers = append([]Server(nil), s.cfg.Servers...)
	fn(&s.cfg)
	if err := s.save(); err != nil {
		s.cfg = before
		return err
	}
	return nil
}

// save 先寫暫存檔再換名，寫到一半當掉也不會留下壞掉的設定檔
func (s *Store) save() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, fileName+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, fileName))
}
