package system

import (
	"os/exec"
	"strings"
	"sync"

	"github.com/yu-ken0207/local-ai-helper/internal/platform"
)

// systemdInhibit 用 systemd-inhibit 擋住睡眠與閒置自動睡眠；放開就結束那個程式。
// 開始時不等它（聊天不能因此變慢）；它自己結束（例如沒有權限）就記下原因。
type systemdInhibit struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	lastErr string
}

func newInhibitor() Inhibitor { return &systemdInhibit{} }

func (s *systemdInhibit) Start() error {
	bin, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "--what=sleep:idle", "--who=Local AI Helper", "--why=A phone is using this computer", "--mode=block", "sleep", "infinity")
	var out strings.Builder
	cmd.Stderr = &out
	platform.PrepareChild(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cmd != cmd {
			return // 是我們關掉的
		}
		s.cmd = nil
		s.lastErr = strings.TrimSpace(out.String())
		if s.lastErr == "" {
			s.lastErr = "systemd-inhibit exited"
		}
	}()
	return nil
}

func (s *systemdInhibit) Stop() {
	s.mu.Lock()
	cmd := s.cmd
	s.cmd = nil
	s.mu.Unlock()
	if cmd != nil {
		_ = cmd.Process.Kill()
	}
}

// Err 最近一次擋不住的原因（例如 Access denied：不是從桌面登入開的）
func (s *systemdInhibit) Err() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return ""
	}
	return s.lastErr
}
