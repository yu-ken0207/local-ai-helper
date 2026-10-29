package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// HideWindow 小幫手沒有主控台視窗，執行其他程式時不要跳出黑色視窗
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// DiskFree 這個資料夾所在的磁碟還有多少空間（位元組）
func DiskFree(dir string) (int64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return int64(free), nil
}

// DataDir 放大檔案的地方（不跟著帳號漫遊）：%LOCALAPPDATA%
func DataDir() (string, error) {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Local"), err
}

var (
	jobOnce sync.Once
	job     windows.Handle
)

// PrepareChild 小幫手開的程式：不跳出視窗
func PrepareChild(cmd *exec.Cmd) { HideWindow(cmd) }

// AdoptChild 把子程式（和它再開的程式）放進「小幫手結束就一起結束」的工作群組，小幫手當掉也不會留下 Ollama
func AdoptChild(cmd *exec.Cmd) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(h)
			return
		}
		job = h
	})
	if job == 0 || cmd.Process == nil {
		return
	}
	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(p)
	_ = windows.AssignProcessToJobObject(job, p)
}

// StopChild Windows 沒有溫和的結束訊號，直接結束
func StopChild(p *os.Process) { _ = p.Kill() }
