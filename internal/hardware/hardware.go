// Package hardware 這台電腦的記憶體與顯示卡（手機依此推薦跑得動的模型）。
package hardware

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GPU struct {
	Name string `json:"name"`
	// VRAMBytes 顯示卡記憶體；0 代表跟主記憶體共用（DGX Spark、Jetson）或查不到
	VRAMBytes int64 `json:"vram_bytes"`
}

type Info struct {
	// MemoryBytes 主記憶體總量；0 代表查不到
	MemoryBytes int64 `json:"memory_bytes"`
	GPUs        []GPU `json:"gpus"`
	// Unified 顯示卡跟主記憶體共用（模型大小看主記憶體）
	Unified bool `json:"unified"`
}

var (
	once   sync.Once
	cached Info
)

// Detect 硬體不會變，第一次查完就記住
func Detect() Info {
	once.Do(func() {
		cached = Info{MemoryBytes: totalMemory(), GPUs: nvidiaGPUs()}
		for _, g := range cached.GPUs {
			if g.VRAMBytes == 0 {
				cached.Unified = true
			}
		}
	})
	return cached
}

// nvidiaGPUs 問 nvidia-smi（沒有 NVIDIA 顯示卡就是空的）
func nvidiaGPUs() []GPU {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return []GPU{}
	}
	return ParseNvidiaSMI(string(out))
}

// ParseNvidiaSMI 每行「名稱, MiB」；共用記憶體的顯示卡回 [N/A]
func ParseNvidiaSMI(out string) []GPU {
	gpus := []GPU{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, mem, ok := strings.Cut(line, ",")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		mib, _ := strconv.ParseInt(strings.TrimSpace(mem), 10, 64)
		gpus = append(gpus, GPU{Name: strings.TrimSpace(name), VRAMBytes: mib << 20})
	}
	return gpus
}
