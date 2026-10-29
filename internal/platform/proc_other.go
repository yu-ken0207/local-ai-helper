//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func HideWindow(*exec.Cmd) {}

// DiskFree 這個資料夾所在的磁碟還有多少空間（位元組）
func DiskFree(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// DataDir 放大檔案的地方：$XDG_DATA_HOME 或 ~/.local/share
func DataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".local", "share"), err
}
