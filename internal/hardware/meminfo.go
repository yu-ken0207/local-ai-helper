package hardware

import (
	"strconv"
	"strings"
)

// ParseMeminfo Linux /proc/meminfo 的 MemTotal（kB）
func ParseMeminfo(s string) int64 {
	for _, line := range strings.Split(s, "\n") {
		if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			f := strings.Fields(rest)
			if len(f) > 0 {
				kb, _ := strconv.ParseInt(f[0], 10, 64)
				return kb << 10
			}
		}
	}
	return 0
}
