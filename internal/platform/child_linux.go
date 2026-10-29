package platform

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// PrepareChild 小幫手結束（包含當掉）時，作業系統會叫子程式也結束
func PrepareChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

func AdoptChild(*exec.Cmd) {}

// StopChild 先請它自己結束（Ollama 會順便關掉載入中的模型），5 秒還沒結束就強制結束
func StopChild(p *os.Process) {
	_ = p.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			if p.Signal(syscall.Signal(0)) != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		close(done)
	}()
	<-done
	_ = p.Kill()
}
