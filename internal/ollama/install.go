// Package ollama 幫沒有 AI 引擎的電腦裝 Ollama，並由小幫手負責開著它。
//
// 不用官方安裝程式（Windows 會多一個系統匣圖示、Linux 要 sudo）：
// 下載 GitHub 上官方發佈的壓縮檔，比對官方的 SHA-256，解到使用者自己的資料夾，
// 由小幫手執行 `ollama serve`。不用管理員權限；要移除就刪掉那個資料夾。
package ollama

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"

	"github.com/yu-ken0207/local-ai-helper/internal/platform"
)

// ReleasesURL Ollama 在 GitHub 的發佈頁（最新版：/latest/download/檔名）
const ReleasesURL = "https://github.com/ollama/ollama/releases"

// Progress 安裝進度
type Progress struct {
	// State idle、downloading、verifying、extracting、starting、done、failed
	State     string `json:"state"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	// Code 失敗的原因代碼：disk_full、checksum、network、extract、unsupported、start_failed
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

// Installer 下載並解開 Ollama
type Installer struct {
	// Dir 裝在哪裡（例如 ~/.local/share/local-ai-helper/ollama）
	Dir string
	// BaseURL 發佈頁（測試可以換成假的）
	BaseURL string
	GOOS    string
	GOARCH  string
	// Extras 額外要疊上去的套件（Jetson 的 jetpack、AMD 顯示卡的 rocm）；nil 就自己偵測
	Extras func() []string
	// DiskFree 磁碟剩多少空間（測試可以換）
	DiskFree func(dir string) (int64, error)
	// AfterInstall 解開之後要做的事（啟動 Ollama）
	AfterInstall func() error

	mu       sync.Mutex
	progress Progress
	running  bool
}

// DefaultDir 小幫手把 Ollama 裝在哪裡
func DefaultDir() (string, error) {
	d, err := platform.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "local-ai-helper", "ollama"), nil
}

func (in *Installer) goos() string {
	if in.GOOS != "" {
		return in.GOOS
	}
	return runtime.GOOS
}

func (in *Installer) goarch() string {
	if in.GOARCH != "" {
		return in.GOARCH
	}
	return runtime.GOARCH
}

// Assets 要下載的檔案（第一個是主程式，後面是疊上去的）
func (in *Installer) Assets() ([]string, error) {
	var base string
	switch in.goos() + "/" + in.goarch() {
	case "linux/amd64", "linux/arm64":
		base = "ollama-linux-" + in.goarch() + ".tar.zst"
	case "windows/amd64", "windows/arm64":
		base = "ollama-windows-" + in.goarch() + ".zip"
	default:
		return nil, fmt.Errorf("this computer (%s/%s) is not supported", in.goos(), in.goarch())
	}
	extras := []string{}
	if in.Extras != nil {
		extras = in.Extras()
	} else if in.goos() == runtime.GOOS {
		extras = detectExtras(in.goarch())
	}
	return append([]string{base}, extras...), nil
}

// Binary 小幫手裝的 Ollama 執行檔；沒裝回空字串
func (in *Installer) Binary() string {
	for _, p := range []string{filepath.Join("bin", "ollama"), "ollama.exe"} {
		full := filepath.Join(in.Dir, p)
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			return full
		}
	}
	return ""
}

func (in *Installer) Progress() Progress {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.progress
}

func (in *Installer) set(fn func(p *Progress)) {
	in.mu.Lock()
	defer in.mu.Unlock()
	fn(&in.progress)
}

// Start 在背景安裝；已經在裝就不重複
func (in *Installer) Start() bool {
	in.mu.Lock()
	if in.running {
		in.mu.Unlock()
		return false
	}
	in.running = true
	in.progress = Progress{State: "downloading"}
	in.mu.Unlock()
	go func() {
		err := in.install(context.Background())
		if err == nil && in.AfterInstall != nil {
			in.set(func(p *Progress) { p.State = "starting" })
			if err = in.AfterInstall(); err != nil {
				err = &installError{"start_failed", err.Error()}
			}
		}
		in.mu.Lock()
		in.running = false
		if err != nil {
			code := "network"
			var ie *installError
			if errors.As(err, &ie) {
				code = ie.code
			}
			in.progress.State, in.progress.Code, in.progress.Error = "failed", code, err.Error()
		} else {
			in.progress.State = "done"
		}
		in.mu.Unlock()
	}()
	return true
}

type installError struct{ code, msg string }

func (e *installError) Error() string { return e.msg }

func (in *Installer) baseURL() string {
	if in.BaseURL != "" {
		return strings.TrimRight(in.BaseURL, "/")
	}
	return ReleasesURL
}

func (in *Installer) install(ctx context.Context) error {
	assets, err := in.Assets()
	if err != nil {
		return &installError{"unsupported", err.Error()}
	}
	sums, err := in.checksums(ctx)
	if err != nil {
		return err
	}
	// 先解到旁邊的資料夾，全部成功才換上去（裝到一半失敗不會留下壞掉的 Ollama）
	parent := filepath.Dir(in.Dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging := in.Dir + ".new"
	_ = os.RemoveAll(staging)
	defer os.RemoveAll(staging)
	for _, asset := range assets {
		want, ok := sums[asset]
		if !ok {
			return &installError{"checksum", "no official checksum for " + asset}
		}
		archive, err := in.download(ctx, asset, want, parent)
		if err != nil {
			return err
		}
		in.set(func(p *Progress) { p.State = "extracting" })
		err = extract(archive, staging)
		os.Remove(archive)
		if err != nil {
			return &installError{"extract", fmt.Sprintf("extract %s: %v", asset, err)}
		}
	}
	_ = os.RemoveAll(in.Dir)
	return os.Rename(staging, in.Dir)
}

// checksums 官方的 sha256sum.txt：「雜湊  ./檔名」
func (in *Installer) checksums(ctx context.Context) (map[string]string, error) {
	resp, err := get(ctx, in.baseURL()+"/latest/download/sha256sum.txt")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	sums := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 {
			sums[strings.TrimPrefix(f[1], "./")] = strings.ToLower(f[0])
		}
	}
	return sums, sc.Err()
}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, &installError{"network", err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &installError{"network", fmt.Sprintf("%s: %s", url, resp.Status)}
	}
	return resp, nil
}

// download 下載到暫存檔，一邊算 SHA-256；跟官方的不一樣就丟掉
func (in *Installer) download(ctx context.Context, asset, want, dir string) (string, error) {
	resp, err := get(ctx, in.baseURL()+"/latest/download/"+asset)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	size := resp.ContentLength
	// 壓縮檔＋解開後的檔案，留三倍的空間
	if size > 0 {
		diskFree := in.DiskFree
		if diskFree == nil {
			diskFree = platform.DiskFree
		}
		if free, err := diskFree(dir); err == nil && free < size*3 {
			return "", &installError{"disk_full", fmt.Sprintf("need %.1f GB free disk space, only %.1f GB left", float64(size*3)/1e9, float64(free)/1e9)}
		}
	}
	in.set(func(p *Progress) { p.State, p.Total = "downloading", p.Total+max(size, 0) })
	f, err := os.CreateTemp(dir, "ollama-download-*")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(f.Name())
				return "", werr
			}
			h.Write(buf[:n])
			in.set(func(p *Progress) { p.Completed += int64(n) })
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(f.Name())
			return "", &installError{"network", rerr.Error()}
		}
	}
	f.Close()
	in.set(func(p *Progress) { p.State = "verifying" })
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(f.Name())
		return "", &installError{"checksum", asset + " does not match the official checksum"}
	}
	return f.Name(), nil
}

// extract 依副檔名解開；不讓檔案寫到目的地外面
func extract(archive, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 4)
	_, _ = io.ReadFull(f, head)
	_, _ = f.Seek(0, io.SeekStart)
	if string(head[:2]) == "PK" {
		st, _ := f.Stat()
		return extractZip(f, st.Size(), dest)
	}
	z, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer z.Close()
	return extractTar(z, dest)
}

// inside 檔案路徑要在目的地裡面（擋掉 ../ 這種）
func inside(dest, name string) (string, error) {
	p := filepath.Join(dest, filepath.FromSlash(name))
	rel, err := filepath.Rel(dest, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(filepath.FromSlash(name)) {
		return "", fmt.Errorf("unsafe path in archive: %s", name)
	}
	return p, nil
}

func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p, err := inside(dest, hd.Name)
		if err != nil {
			return err
		}
		switch hd.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(p, tr, os.FileMode(hd.Mode)&0o777); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// 函式庫的版本連結（libcudart.so.12 → libcudart.so.12.8.90），只接受指到裡面的
			if filepath.IsAbs(hd.Linkname) {
				return fmt.Errorf("unsafe link in archive: %s", hd.Name)
			}
			if _, err := inside(dest, filepath.ToSlash(filepath.Join(filepath.Dir(hd.Name), hd.Linkname))); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			_ = os.Remove(p)
			if err := os.Symlink(hd.Linkname, p); err != nil {
				return err
			}
		}
	}
}

func extractZip(r io.ReaderAt, size int64, dest string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		p, err := inside(dest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(p, rc, 0o755)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeFile(p string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
