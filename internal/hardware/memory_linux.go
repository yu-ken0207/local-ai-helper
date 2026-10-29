package hardware

import (
	"os"
	"os/exec"
)

func totalMemory() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	return ParseMeminfo(string(data))
}

func hideWindow(*exec.Cmd) {}
