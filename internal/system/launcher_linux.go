package system

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed icon256.png
var appIcon []byte

func dataHome() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// InstallLauncher 把小幫手放進應用程式選單（搜尋「Local AI」找得到、可以釘到 Dock）：
// 結束之後一般使用者不用打指令就能再打開。每次啟動都更新（執行檔搬了位置也跟著改）
func InstallLauncher(exe string) error {
	dir, err := dataHome()
	if err != nil {
		return err
	}
	icon := filepath.Join(dir, "local-ai-helper", "icon.png")
	if err := writeIfChanged(icon, appIcon); err != nil {
		return err
	}
	entry := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Local AI Helper",
		"Name[zh_TW]=Local AI 小幫手",
		"Comment=Let phones at home use the AI models on this computer",
		"Comment[zh_TW]=讓家裡的手機用這台電腦上的 AI 模型",
		"Exec=" + quoteExec(exe),
		"Icon=" + icon,
		"Terminal=false",
		"Categories=Utility;",
		"Keywords=AI;LLM;Ollama;",
		"",
	}, "\n")
	return writeIfChanged(filepath.Join(dir, "applications", "local-ai-helper.desktop"), []byte(entry))
}

func writeIfChanged(p string, data []byte) error {
	if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
