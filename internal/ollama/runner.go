package ollama

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/platform"
)

// Runner 由小幫手開著 Ollama（`ollama serve`）；意外結束會自動重開，短時間一直失敗就放棄
type Runner struct {
	// Binary 要執行的 ollama
	Binary func() string
	// URL Ollama 要聽的位址（例如 http://127.0.0.1:11434）
	URL string
	// LogPath Ollama 的輸出寫到這裡（超過 5 MB 從頭寫）
	LogPath string
	// RestartDelay 意外結束後多久重開（預設 3 秒）
	RestartDelay time.Duration

	mu       sync.Mutex
	cmd      *exec.Cmd
	stopping bool
	failures int
	lastErr  string
}

// ErrNotInstalled 找不到 ollama
var ErrNotInstalled = errors.New("ollama is not installed")

// maxQuickFailures 開起來不到一分鐘就結束，連續幾次就不再重開
const maxQuickFailures = 5

// Start 開 Ollama；已經開著就不動
func (r *Runner) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return nil
	}
	r.stopping = false
	r.failures = 0
	return r.startLocked()
}

func (r *Runner) startLocked() error {
	bin := r.Binary()
	if bin == "" {
		return ErrNotInstalled
	}
	cmd := exec.Command(bin, "serve")
	cmd.Env = os.Environ()
	if u, err := url.Parse(r.URL); err == nil && u.Host != "" {
		cmd.Env = append(cmd.Env, "OLLAMA_HOST="+u.Host)
	}
	if r.LogPath != "" {
		if st, err := os.Stat(r.LogPath); err == nil && st.Size() > 5<<20 {
			_ = os.Remove(r.LogPath)
		}
		if f, err := os.OpenFile(r.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			cmd.Stdout, cmd.Stderr = f, f
			defer f.Close() // 子程式拿到自己的副本
		}
	} else {
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	}
	platform.PrepareChild(cmd)
	if err := cmd.Start(); err != nil {
		r.lastErr = err.Error()
		return fmt.Errorf("start ollama: %w", err)
	}
	platform.AdoptChild(cmd)
	r.cmd = cmd
	r.lastErr = ""
	started := time.Now()
	go r.wait(cmd, started)
	return nil
}

func (r *Runner) wait(cmd *exec.Cmd, started time.Time) {
	err := cmd.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == cmd {
		r.cmd = nil
	}
	if r.stopping {
		return
	}
	if err != nil {
		r.lastErr = err.Error()
	} else {
		r.lastErr = "ollama exited"
	}
	if time.Since(started) < time.Minute {
		r.failures++
	} else {
		r.failures = 0
	}
	if r.failures >= maxQuickFailures {
		log.Printf("Ollama 一直開不起來，不再重開：%s", r.lastErr)
		return
	}
	delay := r.RestartDelay
	if delay == 0 {
		delay = 3 * time.Second
	}
	log.Printf("Ollama 結束了（%s），%v 後重開", r.lastErr, delay)
	time.AfterFunc(delay, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if !r.stopping && r.cmd == nil {
			_ = r.startLocked()
		}
	})
}

// Stop 關掉小幫手開的 Ollama（小幫手結束時）
func (r *Runner) Stop() {
	r.mu.Lock()
	r.stopping = true
	cmd := r.cmd
	r.cmd = nil
	r.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		platform.StopChild(cmd.Process)
	}
}

// Managing 小幫手是不是正開著 Ollama
func (r *Runner) Managing() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cmd != nil
}

// LastError 最近一次結束或開不起來的原因
func (r *Runner) LastError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}
