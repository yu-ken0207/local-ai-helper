package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesktopEntryQuoting(t *testing.T) {
	e := DesktopEntry(`/home/a b/$x/local-ai-helper`)
	if !strings.Contains(e, `Exec="/home/a b/\\$x/local-ai-helper" --no-browser`) {
		t.Fatalf("空白與 $ 要照 .desktop 的規則：%s", e)
	}
}

func TestAutostartFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if AutostartEnabled() {
		t.Fatal("一開始沒有")
	}
	if err := SetAutostart(true, "/opt/lai/local-ai-helper"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart", "local-ai-helper.desktop"))
	if !AutostartEnabled() || !strings.Contains(string(data), `Exec="/opt/lai/local-ai-helper" --no-browser`) {
		t.Fatalf("%s", data)
	}
	if err := SetAutostart(false, ""); err != nil || AutostartEnabled() {
		t.Fatal("關掉要刪檔")
	}
	if err := SetAutostart(false, ""); err != nil {
		t.Fatal("已經關了再關不算錯")
	}
}

func TestFirewallLinux(t *testing.T) {
	dir := t.TempDir()
	ufwConf, ufwRules, procCommGlb = filepath.Join(dir, "ufw.conf"), filepath.Join(dir, "user.rules"), filepath.Join(dir, "proc", "*", "comm")
	t.Cleanup(func() {
		ufwConf, ufwRules, procCommGlb = "/etc/ufw/ufw.conf", "/etc/ufw/user.rules", "/proc/[0-9]*/comm"
	})

	if st := Firewall(11480, ""); st.Kind != "" || st.Open != "yes" {
		t.Fatalf("沒有防火牆：%+v", st)
	}
	_ = os.WriteFile(ufwConf, []byte("# comment\nENABLED=yes\n"), 0o644)
	if st := Firewall(11480, ""); st.Kind != "ufw" || st.Open != "unknown" || st.Command != "sudo ufw allow 11480/tcp" {
		t.Fatalf("讀不到規則：%+v", st)
	}
	_ = os.WriteFile(ufwRules, []byte("-A ufw-user-input -p tcp --dport 11434 -j ACCEPT\n"), 0o644)
	if st := Firewall(11480, ""); st.Open != "no" {
		t.Fatalf("%+v", st)
	}
	_ = os.WriteFile(ufwRules, []byte("-A ufw-user-input -p tcp --dport 11480 -j ACCEPT\n"), 0o644)
	if st := Firewall(11480, ""); st.Open != "yes" {
		t.Fatalf("%+v", st)
	}
	_ = os.WriteFile(ufwConf, []byte("ENABLED=no\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "proc", "42"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "proc", "42", "comm"), []byte("firewalld\n"), 0o644)
	if st := Firewall(11480, ""); st.Kind != "firewalld" || !strings.Contains(st.Command, "--add-port=11480/tcp") {
		t.Fatalf("%+v", st)
	}
}

func TestSystemdInhibitHere(t *testing.T) {
	inh := newInhibitor().(*systemdInhibit)
	if err := inh.Start(); err != nil {
		t.Skip("沒有 systemd-inhibit：", err)
	}
	a := &Awake{inh: inh, on: true}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// 有權限就一直擋著；沒權限（不是從桌面登入開的）要說出原因
		if st := a.Status(); st.Error != "" {
			t.Logf("這個環境不能擋睡眠：%s", st.Error)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	inh.Stop()
	if inh.Err() != "" {
		t.Fatal("我們自己關掉的不算錯")
	}
}
