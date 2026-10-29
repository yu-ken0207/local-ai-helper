package system

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// autostartFile 桌面登入時自動打開的程式（XDG autostart，GNOME、KDE 等都認得）
func autostartFile() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "local-ai-helper.desktop"), nil
}

// SetAutostart 打開或關掉開機自動啟動；打開時每次都重寫（執行檔搬了位置也跟著改）
func SetAutostart(enabled bool, exe string) error {
	p, err := autostartFile()
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(DesktopEntry(exe)), 0o644)
}

// AutostartEnabled 現在有沒有設定開機自動啟動
func AutostartEnabled() bool {
	p, err := autostartFile()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// DesktopEntry 自動啟動的 .desktop 內容
func DesktopEntry(exe string) string {
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Local AI Helper",
		"Name[zh_TW]=Local AI 小幫手",
		"Comment=Let phones at home use the AI models on this computer",
		"Exec=" + quoteExec(exe) + " " + autostartArgs,
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
}

// quoteExec .desktop 的 Exec 引號規則：雙引號裡的 " ` $ \ 前面加反斜線，整行再把反斜線加倍
func quoteExec(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return strings.ReplaceAll(`"`+r.Replace(s)+`"`, `\`, `\\`)
}
