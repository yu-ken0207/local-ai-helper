// Package system 跟作業系統打交道的三件事：開機自動啟動、手機使用中不讓電腦睡著、防火牆。
package system

import (
	"os"
	"path/filepath"
)

// Executable 小幫手自己的執行檔（開機啟動、防火牆規則都指到它）
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

// autostartArgs 開機時打開：不跳出管理頁（使用者沒有要看）
const autostartArgs = "--no-browser"
