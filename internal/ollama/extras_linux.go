package ollama

import (
	"os"
	"path/filepath"
	"strings"
)

// detectExtras 跟官方 install.sh 一樣：Jetson 疊 jetpack 套件、AMD 顯示卡疊 rocm 套件
func detectExtras(arch string) []string {
	if data, err := os.ReadFile("/etc/nv_tegra_release"); err == nil {
		switch {
		case strings.Contains(string(data), "R36"):
			return []string{"ollama-linux-" + arch + "-jetpack6.tar.zst"}
		case strings.Contains(string(data), "R35"):
			return []string{"ollama-linux-" + arch + "-jetpack5.tar.zst"}
		}
	}
	if arch == "amd64" {
		vendors, _ := filepath.Glob("/sys/class/drm/card*/device/vendor")
		for _, v := range vendors {
			if data, err := os.ReadFile(v); err == nil && strings.TrimSpace(string(data)) == "0x1002" {
				return []string{"ollama-linux-amd64-rocm.tar.zst"}
			}
		}
	}
	return nil
}
