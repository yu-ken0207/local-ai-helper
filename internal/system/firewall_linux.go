package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 測試可以換
var (
	ufwConf     = "/etc/ufw/ufw.conf"
	ufwRules    = "/etc/ufw/user.rules"
	procCommGlb = "/proc/[0-9]*/comm"
)

// Firewall 看 ufw（Ubuntu）與 firewalld（Fedora）有沒有開；一般使用者通常讀不到規則，只能說「不確定」
func Firewall(port int, _ string) FirewallStatus {
	if data, err := os.ReadFile(ufwConf); err == nil && regexp.MustCompile(`(?m)^\s*ENABLED\s*=\s*yes`).Match(data) {
		st := FirewallStatus{Kind: "ufw", Open: "unknown", Command: fmt.Sprintf("sudo ufw allow %d/tcp", port)}
		if rules, err := os.ReadFile(ufwRules); err == nil {
			st.Open = "no"
			if regexp.MustCompile(fmt.Sprintf(`(?m)^-A ufw-user-input .*--dport %d\b.*-j ACCEPT`, port)).Match(rules) {
				st.Open = "yes"
			}
		}
		return st
	}
	if running("firewalld") {
		return FirewallStatus{Kind: "firewalld", Open: "unknown", Command: fmt.Sprintf("sudo firewall-cmd --permanent --add-port=%d/tcp && sudo firewall-cmd --reload", port)}
	}
	return FirewallStatus{Open: "yes"}
}

func running(name string) bool {
	comms, _ := filepath.Glob(procCommGlb)
	for _, c := range comms {
		if data, err := os.ReadFile(c); err == nil && strings.TrimSpace(string(data)) == name {
			return true
		}
	}
	return false
}

// AllowFirewall Linux 改防火牆要 sudo，小幫手不代勞（管理頁給指令讓使用者自己執行）
func AllowFirewall(int, string) error {
	return errors.New("run the command shown on the page")
}
