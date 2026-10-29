package ollama

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Manager 管理頁用：裝 Ollama、開 Ollama
type Manager struct {
	Installer *Installer
	Runner    *Runner
	// URL Ollama 的位址（http://127.0.0.1:11434）
	URL string
	// LookPath 找已經裝在電腦上的 ollama（測試可以換）
	LookPath func() string
}

// Status 管理頁顯示的 Ollama 狀態（有沒有開著由引擎清單另外說）
type Status struct {
	// Installed helper：小幫手裝的；system：使用者自己裝的；空的：沒裝
	Installed string `json:"installed"`
	// Managing 小幫手正開著它
	Managing bool     `json:"managing"`
	Install  Progress `json:"install"`
	// Error 小幫手開的 Ollama 最近一次結束的原因
	Error string `json:"error,omitempty"`
}

// NewManager 照預設位置組好
func NewManager(dir, ollamaURL, logPath string) *Manager {
	m := &Manager{URL: ollamaURL, LookPath: systemOllama}
	m.Installer = &Installer{Dir: dir}
	m.Runner = &Runner{URL: ollamaURL, LogPath: logPath, Binary: m.binary}
	m.Installer.AfterInstall = func() error {
		if err := m.Runner.Start(); err != nil {
			return err
		}
		return m.waitUp(60 * time.Second)
	}
	return m
}

func (m *Manager) binary() string {
	if b := m.Installer.Binary(); b != "" {
		return b
	}
	return m.LookPath()
}

func (m *Manager) Status() Status {
	st := Status{Install: m.Installer.Progress(), Managing: m.Runner.Managing()}
	switch {
	case m.Installer.Binary() != "":
		st.Installed = "helper"
	case m.LookPath() != "":
		st.Installed = "system"
	}
	if !st.Managing {
		st.Error = m.Runner.LastError()
	}
	return st
}

// ErrBusy 正在安裝
var ErrBusy = errors.New("ollama is being installed")

// Install 開始安裝（已經在裝回 false）
func (m *Manager) Install() bool {
	return m.Installer.Start()
}

// Start 開 Ollama：小幫手裝的、或電腦上本來就有的
func (m *Manager) Start() error {
	if m.Installer.Progress().State == "downloading" || m.Installer.Progress().State == "extracting" {
		return ErrBusy
	}
	// Windows 使用者自己裝的 Ollama：打開它自己的程式（有系統匣、會自動更新），不由小幫手管
	if m.Installer.Binary() == "" && runtime.GOOS == "windows" {
		if app := filepath.Join(filepath.Dir(m.LookPath()), "ollama app.exe"); m.LookPath() != "" && exists(app) {
			if err := exec.Command(app).Start(); err != nil {
				return err
			}
			return m.waitUp(30 * time.Second)
		}
	}
	if err := m.Runner.Start(); err != nil {
		return err
	}
	return m.waitUp(30 * time.Second)
}

// Boot 小幫手啟動時：小幫手裝的 Ollama 沒開著就開起來
func (m *Manager) Boot(ctx context.Context) {
	if m.Installer.Binary() == "" || m.Up(ctx) {
		return
	}
	_ = m.Runner.Start()
}

// Up Ollama 有沒有在回應
func (m *Manager) Up(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(m.URL, "/")+"/api/version", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitUp 等 Ollama 開始回應
func (m *Manager) waitUp(limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if m.Up(context.Background()) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	if e := m.Runner.LastError(); e != "" {
		return errors.New("ollama did not start: " + e)
	}
	return errors.New("ollama did not start in time")
}

// systemOllama 使用者自己裝的 ollama（PATH 裡，或 Windows 預設的安裝位置）
func systemOllama() string {
	if p, err := exec.LookPath("ollama"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			if p := filepath.Join(d, "Programs", "Ollama", "ollama.exe"); exists(p) {
				return p
			}
		}
	}
	return ""
}

func exists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
