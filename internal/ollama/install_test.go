package ollama

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

type entry struct {
	name, body, link string
	mode             int64
}

func tarZst(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		hd := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.link != "" {
			hd.Typeflag, hd.Linkname, hd.Size = tar.TypeSymlink, e.link, 0
		}
		if strings.HasSuffix(e.name, "/") {
			hd.Typeflag, hd.Size = tar.TypeDir, 0
		}
		if err := tw.WriteHeader(hd); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(e.body))
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func zipFile(t *testing.T, entries []entry) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, _ := zw.Create(e.name)
		_, _ = w.Write([]byte(e.body))
	}
	zw.Close()
	return buf.Bytes()
}

// release 假的 GitHub 發佈頁：sha256sum.txt 照實際內容算（wrong 裡的檔案故意算錯）
func release(t *testing.T, files map[string][]byte, wrong ...string) *httptest.Server {
	t.Helper()
	var sums strings.Builder
	for name, data := range files {
		h := sha256.Sum256(data)
		sum := hex.EncodeToString(h[:])
		for _, w := range wrong {
			if w == name {
				sum = strings.Repeat("0", 64)
			}
		}
		fmt.Fprintf(&sums, "%s  ./%s\n", sum, name)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/latest/download/")
		if name == "sha256sum.txt" {
			fmt.Fprint(w, sums.String())
			return
		}
		data, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitDone(t *testing.T, in *Installer) Progress {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p := in.Progress(); p.State == "done" || p.State == "failed" {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("安裝一直沒結束")
	return Progress{}
}

func plenty(string) (int64, error) { return 1 << 40, nil }

func TestInstallLinux(t *testing.T) {
	base := tarZst(t, []entry{
		{name: "bin/", mode: 0o755},
		{name: "bin/ollama", body: "#!/bin/sh\n", mode: 0o755},
		{name: "lib/ollama/libcudart.so.12.8.90", body: "lib", mode: 0o644},
		{name: "lib/ollama/libcudart.so.12", link: "libcudart.so.12.8.90"},
	})
	jetpack := tarZst(t, []entry{{name: "lib/ollama/cuda_jetpack6/libggml-cuda.so", body: "jp", mode: 0o644}})
	srv := release(t, map[string][]byte{"ollama-linux-arm64.tar.zst": base, "ollama-linux-arm64-jetpack6.tar.zst": jetpack})
	dir := filepath.Join(t.TempDir(), "ollama")
	in := &Installer{Dir: dir, BaseURL: srv.URL, GOOS: "linux", GOARCH: "arm64", DiskFree: plenty, Extras: func() []string { return []string{"ollama-linux-arm64-jetpack6.tar.zst"} }}
	if !in.Start() {
		t.Fatal("要開始安裝")
	}
	if in.Start() {
		t.Fatal("安裝中不能再開始一次")
	}
	p := waitDone(t, in)
	if p.State != "done" || p.Completed != int64(len(base)+len(jetpack)) || p.Total != p.Completed {
		t.Fatalf("%+v", p)
	}
	if in.Binary() != filepath.Join(dir, "bin", "ollama") {
		t.Fatalf("找不到執行檔：%q", in.Binary())
	}
	// Windows 沒有「可以執行」的權限位元，只在其他系統檢查
	if st, _ := os.Stat(in.Binary()); runtime.GOOS != "windows" && st.Mode()&0o100 == 0 {
		t.Fatal("執行檔要可以執行")
	}
	if target, _ := os.Readlink(filepath.Join(dir, "lib/ollama/libcudart.so.12")); target != "libcudart.so.12.8.90" {
		t.Fatal("函式庫的版本連結要保留")
	}
	if _, err := os.Stat(filepath.Join(dir, "lib/ollama/cuda_jetpack6/libggml-cuda.so")); err != nil {
		t.Fatal("jetpack 套件要疊上去")
	}
	// 下載的暫存檔、解到一半的資料夾都不留
	left, _ := os.ReadDir(filepath.Dir(dir))
	if len(left) != 1 {
		t.Fatalf("留下多餘的檔案：%v", left)
	}
}

func TestInstallWindowsZip(t *testing.T) {
	data := zipFile(t, []entry{{name: "ollama.exe", body: "MZ"}, {name: "lib/ollama/ggml.dll", body: "dll"}})
	srv := release(t, map[string][]byte{"ollama-windows-amd64.zip": data})
	dir := filepath.Join(t.TempDir(), "ollama")
	in := &Installer{Dir: dir, BaseURL: srv.URL, GOOS: "windows", GOARCH: "amd64", DiskFree: plenty, Extras: func() []string { return nil }}
	in.Start()
	if p := waitDone(t, in); p.State != "done" || in.Binary() != filepath.Join(dir, "ollama.exe") {
		t.Fatalf("%+v %q", p, in.Binary())
	}
}

func TestInstallRefusesBadDownloads(t *testing.T) {
	good := tarZst(t, []entry{{name: "bin/ollama", body: "x", mode: 0o755}})
	evil := tarZst(t, []entry{{name: "../../evil", body: "x", mode: 0o644}})
	evilLink := tarZst(t, []entry{{name: "bin/ollama", link: "/etc/passwd"}})
	evilBackslash := tarZst(t, []entry{{name: "bin/ollama", link: `\Windows\System32\evil.dll`}})
	cases := []struct {
		name  string
		files map[string][]byte
		wrong []string
		free  int64
		code  string
	}{
		{"檢查碼不對", map[string][]byte{"ollama-linux-amd64.tar.zst": good}, []string{"ollama-linux-amd64.tar.zst"}, 1 << 40, "checksum"},
		{"磁碟不夠", map[string][]byte{"ollama-linux-amd64.tar.zst": good}, nil, 10, "disk_full"},
		{"路徑跑到外面", map[string][]byte{"ollama-linux-amd64.tar.zst": evil}, nil, 1 << 40, "extract"},
		{"連結指到外面", map[string][]byte{"ollama-linux-amd64.tar.zst": evilLink}, nil, 1 << 40, "extract"},
		{"連結從根目錄開始（Windows 寫法）", map[string][]byte{"ollama-linux-amd64.tar.zst": evilBackslash}, nil, 1 << 40, "extract"},
		{"官方沒有這個檔案", map[string][]byte{}, nil, 1 << 40, "checksum"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := release(t, c.files, c.wrong...)
			root := t.TempDir()
			dir := filepath.Join(root, "share", "ollama")
			free := c.free
			in := &Installer{Dir: dir, BaseURL: srv.URL, GOOS: "linux", GOARCH: "amd64", Extras: func() []string { return nil }, DiskFree: func(string) (int64, error) { return free, nil }}
			in.Start()
			p := waitDone(t, in)
			if p.State != "failed" || p.Code != c.code {
				t.Fatalf("%+v", p)
			}
			if in.Binary() != "" {
				t.Fatal("失敗不能留下 Ollama")
			}
			if _, err := os.Stat(filepath.Join(root, "evil")); err == nil {
				t.Fatal("寫到外面了")
			}
		})
	}
}

func TestRootedPaths(t *testing.T) {
	for _, name := range []string{"/etc/passwd", `\Windows\evil`, "/"} {
		if !rooted(name) {
			t.Fatalf("%q 要算從根目錄開始", name)
		}
	}
	for _, name := range []string{"bin/ollama", "libcudart.so.12.8.90", "../lib/x"} {
		if rooted(name) {
			t.Fatalf("%q 是相對路徑", name)
		}
	}
	if runtime.GOOS == "windows" && !rooted(`C:\evil`) {
		t.Fatal("Windows 上 C:\\evil 要算絕對路徑")
	}
}

func TestUnsupportedComputer(t *testing.T) {
	in := &Installer{Dir: t.TempDir(), GOOS: "linux", GOARCH: "386"}
	in.Start()
	if p := waitDone(t, in); p.Code != "unsupported" {
		t.Fatalf("%+v", p)
	}
}

// fakeOllama 假的 ollama：記下自己的 PID，一直跑；exitNow 就馬上結束
func fakeOllama(t *testing.T, exitNow bool) (bin, pidFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("用 shell script 假裝 ollama")
	}
	dir := t.TempDir()
	pidFile = filepath.Join(dir, "pids")
	script := fmt.Sprintf("#!/bin/sh\necho $$ >> %s\necho \"$@ $OLLAMA_HOST\" > %s.args\n", pidFile, pidFile)
	if !exitNow {
		script += "exec sleep 60\n"
	}
	bin = filepath.Join(dir, "ollama")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, pidFile
}

func pids(pidFile string) []string {
	data, _ := os.ReadFile(pidFile)
	return strings.Fields(string(data))
}

func TestRunnerRestartsAndStops(t *testing.T) {
	bin, pidFile := fakeOllama(t, false)
	r := &Runner{Binary: func() string { return bin }, URL: "http://127.0.0.1:11434", RestartDelay: 10 * time.Millisecond}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(pids(pidFile)) == 1 })
	args, _ := os.ReadFile(pidFile + ".args")
	if strings.TrimSpace(string(args)) != "serve 127.0.0.1:11434" {
		t.Fatalf("要用 serve 開、聽指定的位址：%q", args)
	}
	// 意外結束：自動重開
	p, _ := os.FindProcess(atoi(pids(pidFile)[0]))
	_ = p.Kill()
	waitFor(t, func() bool { return len(pids(pidFile)) == 2 && r.Managing() })
	// 小幫手結束：關掉，不再重開
	r.Stop()
	time.Sleep(100 * time.Millisecond)
	if r.Managing() || len(pids(pidFile)) != 2 {
		t.Fatalf("關掉後不能再開：%v", pids(pidFile))
	}
}

func TestRunnerGivesUpWhenItKeepsFailing(t *testing.T) {
	bin, pidFile := fakeOllama(t, true)
	r := &Runner{Binary: func() string { return bin }, URL: "http://127.0.0.1:11434", RestartDelay: 5 * time.Millisecond}
	_ = r.Start()
	waitFor(t, func() bool { return len(pids(pidFile)) == maxQuickFailures })
	time.Sleep(100 * time.Millisecond)
	if len(pids(pidFile)) != maxQuickFailures || r.Managing() || r.LastError() == "" {
		t.Fatalf("一直失敗要放棄：%d 次", len(pids(pidFile)))
	}
	if err := (&Runner{Binary: func() string { return "" }}).Start(); err != ErrNotInstalled {
		t.Fatal(err)
	}
}

func TestManagerStatusAndBoot(t *testing.T) {
	bin, pidFile := fakeOllama(t, false)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"version":"0.40.12"}`) }))
	defer up.Close()
	m := NewManager(filepath.Join(t.TempDir(), "ollama"), "http://127.0.0.1:1", "")
	m.LookPath = func() string { return "" }
	if st := m.Status(); st.Installed != "" || st.Managing {
		t.Fatalf("%+v", st)
	}
	m.LookPath = func() string { return bin }
	if st := m.Status(); st.Installed != "system" {
		t.Fatalf("自己裝的 Ollama：%+v", st)
	}
	// 小幫手裝的 Ollama：開機時沒開著就開起來
	if err := os.MkdirAll(filepath.Join(m.Installer.Dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(bin)
	_ = os.WriteFile(filepath.Join(m.Installer.Dir, "bin", "ollama"), data, 0o755)
	m.Boot(t.Context())
	waitFor(t, func() bool { return m.Status().Managing && m.Status().Installed == "helper" })
	m.Runner.Stop()
	// 已經開著（例如使用者自己開了）就不重複開
	m.URL = up.URL
	before := len(pids(pidFile))
	m.Boot(t.Context())
	time.Sleep(50 * time.Millisecond)
	if m.Status().Managing || len(pids(pidFile)) != before {
		t.Fatal("Ollama 已經開著不用再開")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等不到")
}

func atoi(s string) int {
	n := 0
	fmt.Sscan(s, &n)
	return n
}
