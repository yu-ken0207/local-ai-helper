package system

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/yu-ken0207/local-ai-helper/internal/platform"
)

// 問 Windows 的結果記一下（netsh、PowerShell 每次要零點幾秒）
var fwCache struct {
	sync.Mutex
	at time.Time
	st FirewallStatus
}

// Firewall 看有沒有小幫手的放行規則、目前的網路是不是被當成公用網路
func Firewall(port int, exe string) FirewallStatus {
	fwCache.Lock()
	defer fwCache.Unlock()
	if time.Since(fwCache.at) < 30*time.Second {
		return fwCache.st
	}
	st := FirewallStatus{Kind: "windows", Open: "no", CanFix: true}
	if ruleExists() {
		st.Open, st.CanFix = "yes", false
	}
	st.PublicNetwork = publicNetwork()
	fwCache.at, fwCache.st = time.Now(), st
	return st
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	platform.HideWindow(cmd)
	out, err := cmd.Output()
	return string(out), err
}

func ruleExists() bool {
	out, err := run("netsh", "advfirewall", "firewall", "show", "rule", "name="+RuleName, "dir=in")
	return err == nil && strings.Contains(out, RuleName)
}

func publicNetwork() bool {
	out, err := run("powershell", "-NoProfile", "-NonInteractive", "-Command", "(Get-NetConnectionProfile).NetworkCategory")
	return err == nil && strings.Contains(out, "Public")
}

// AllowFirewall 跳出系統的權限確認，加一條只在家用（私人）與公司網路放行的規則。
// 先刪掉這個程式的其他規則：第一次開時如果在 Windows 的防火牆提示按了取消，會留下封鎖規則，封鎖比放行優先。
func AllowFirewall(port int, exe string) error {
	if strings.ContainsAny(exe, `"&|^<>%`) {
		return errors.New("the helper's folder name has characters the firewall command can't use")
	}
	args := fmt.Sprintf(`/c netsh advfirewall firewall delete rule name=all dir=in program="%s" & netsh advfirewall firewall add rule name="%s" dir=in action=allow program="%s" protocol=TCP localport=%d profile=private,domain`, exe, RuleName, exe, port)
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString("cmd.exe")
	params, _ := windows.UTF16PtrFromString(args)
	if err := windows.ShellExecute(0, verb, file, params, nil, windows.SW_HIDE); err != nil {
		return err // 使用者在權限確認按了「否」也會到這裡
	}
	// 系統的權限確認不會等，自己問到規則出現為止
	for i := 0; i < 60; i++ {
		if ruleExists() {
			fwCache.Lock()
			fwCache.at = time.Time{}
			fwCache.Unlock()
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("the firewall rule was not added")
}
