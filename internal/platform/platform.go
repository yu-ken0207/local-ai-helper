// Package platform 放跟作業系統有關的小功能：打開瀏覽器、判斷介面語言。
package platform

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OpenBrowser 用預設瀏覽器打開網址
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// Chinese 介面要不要用中文（系統語言是中文就用）
func Chinese() bool {
	for _, l := range uiLanguages() {
		if strings.HasPrefix(strings.ToLower(l), "zh") {
			return true
		}
		if l != "" {
			return false
		}
	}
	return false
}

func envLanguages() []string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
			return []string{v}
		}
	}
	return nil
}

// HasDesktop 這台 Linux 有沒有桌面（沒有就不放系統匣圖示、不開瀏覽器）
func HasDesktop() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}
