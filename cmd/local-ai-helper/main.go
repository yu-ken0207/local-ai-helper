// Local AI 小幫手：讓家裡的手機用這台電腦上的 AI 模型。
// 點兩下打開：開一個只有本機看得到的管理頁，系統匣放一個圖示；已經開著就只把管理頁叫出來。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/admin"
	"github.com/yu-ken0207/local-ai-helper/internal/config"
	"github.com/yu-ken0207/local-ai-helper/internal/engine"
	"github.com/yu-ken0207/local-ai-helper/internal/gateway"
	"github.com/yu-ken0207/local-ai-helper/internal/netinfo"
	"github.com/yu-ken0207/local-ai-helper/internal/ollama"
	"github.com/yu-ken0207/local-ai-helper/internal/pairing"
	"github.com/yu-ken0207/local-ai-helper/internal/platform"
	"github.com/yu-ken0207/local-ai-helper/internal/system"
	"github.com/yu-ken0207/local-ai-helper/internal/tray"

	// 系統沒有根憑證時（精簡的 Linux）用 Go 內建的一份，下載 Ollama 才連得上 HTTPS
	_ "golang.org/x/crypto/x509roots/fallback"
)

// version 編譯時用 -ldflags "-X main.version=..." 填入
var version = "dev"

func main() {
	noBrowser := flag.Bool("no-browser", false, "啟動時不打開管理頁")
	noTray := flag.Bool("no-tray", false, "不放系統匣圖示（沒有桌面的電腦）")
	configDir := flag.String("config-dir", "", "設定資料夾（預設是使用者的設定資料夾）")
	flag.Parse()

	dir := *configDir
	if dir == "" {
		d, err := config.Dir()
		if err != nil {
			fatal(err)
		}
		dir = d
	}
	setupLog(dir)

	store, err := config.Open(dir)
	if err != nil {
		fatal(fmt.Errorf("讀不到設定：%w", err))
	}
	cfg := store.Get()
	adminURL := admin.URL(cfg.AdminPort)
	showPage := !*noBrowser && platform.HasDesktop()

	// 開機自動啟動（預設開；用 --config-dir 開的是測試，不動開機設定）
	exe, err := system.Executable()
	if err != nil {
		log.Printf("找不到自己的執行檔：%v", err)
	}
	setAutostart := system.SetAutostart
	if *configDir != "" {
		setAutostart = nil
	} else if exe != "" {
		if err := system.SetAutostart(!cfg.NoAutostart, exe); err != nil {
			log.Printf("設定開機自動啟動失敗：%v", err)
		}
		// 應用程式選單／開始功能表：結束之後不用打指令就能再打開（已經有一個在跑也照樣放）
		if err := system.InstallLauncher(exe); err != nil {
			log.Printf("放進應用程式選單失敗：%v", err)
		}
	}

	// 只開一個：連接埠已經被自己佔住，就把管理頁叫出來後結束
	adminLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.AdminPort))
	if err != nil {
		if alreadyRunning(adminURL) {
			log.Printf("小幫手已經在執行，打開管理頁")
			if showPage {
				_ = platform.OpenBrowser(adminURL)
			}
			return
		}
		fatal(fmt.Errorf("連接埠 %d 被別的程式佔用：%w", cfg.AdminPort, err))
	}

	var once sync.Once
	quitCh := make(chan struct{})
	quit := func() { once.Do(func() { close(quitCh) }) }

	pair := pairing.New(store)
	registry := &engine.Registry{
		LMStudio: &engine.LMStudio{BaseURL: cfg.LMStudioURL, APIKey: cfg.LMStudioToken},
		Servers: func() []engine.Server {
			out := []engine.Server{}
			for _, sv := range store.Get().Servers {
				out = append(out, engine.Server{ID: sv.ID, Name: sv.Name, URL: sv.URL, APIKey: sv.APIKey})
			}
			return out
		},
	}
	gw := &gateway.Server{Store: store, Pairing: pair, Version: version, OllamaURL: cfg.OllamaURL, Registry: registry}

	// 手機使用中不讓電腦睡著
	awake := system.NewAwake()
	awakeCtx, stopAwake := context.WithCancel(context.Background())
	go awake.Run(awakeCtx)
	defer stopAwake()
	defer awake.Stop()
	gw.Awake = awake

	// 小幫手裝的 Ollama：開著小幫手就開著它（結束時一起關）
	var olm *ollama.Manager
	if olDir, err := ollama.DefaultDir(); err == nil {
		olm = ollama.NewManager(olDir, cfg.OllamaURL, filepath.Join(dir, "ollama.log"))
		// 開發測試用：從假的發佈頁下載
		olm.Installer.BaseURL = os.Getenv("LOCAL_AI_HELPER_OLLAMA_RELEASES")
		go olm.Boot(context.Background())
		defer olm.Runner.Stop()
	} else {
		log.Printf("找不到放 Ollama 的資料夾：%v", err)
	}

	// 手機用的門口（聽區網）；開不起來就在管理頁說明原因，管理頁照常可以用
	var apiErr struct {
		sync.Mutex
		msg string
	}
	apiSrv := &http.Server{Handler: gw.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if apiLn, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.APIPort)); err != nil {
		apiErr.msg = err.Error()
		log.Printf("手機用的連接埠 %d 開不起來：%v", cfg.APIPort, err)
	} else {
		go func() {
			if err := apiSrv.Serve(apiLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				apiErr.Lock()
				apiErr.msg = err.Error()
				apiErr.Unlock()
				log.Printf("手機用的門口停止：%v", err)
			}
		}()
	}

	adminSrv := &http.Server{
		Handler: (&admin.Server{
			Store: store, Pairing: pair, Version: version, Quit: quit, Addrs: netinfo.LAN, Engines: gw.Engines, Redetect: registry.Redetect, Ollama: olm,
			Exe: exe, SetAutostart: setAutostart, Firewall: system.Firewall, AllowFirewall: system.AllowFirewall, Awake: awake, LANSeen: gw.LANSeen,
			APIError: func() string {
				apiErr.Lock()
				defer apiErr.Unlock()
				return apiErr.msg
			},
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := adminSrv.Serve(adminLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("管理頁停止：%v", err)
			quit()
		}
	}()
	log.Printf("小幫手 %s 啟動，管理頁 %s，設定 %s", version, adminURL, dir)

	if showPage {
		if err := platform.OpenBrowser(adminURL); err != nil {
			log.Printf("打不開瀏覽器：%v", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		quit()
	}()

	if !*noTray && platform.HasDesktop() {
		go func() {
			<-quitCh
			tray.Quit()
		}()
		tray.Run(trayLabels(), func() { _ = platform.OpenBrowser(adminURL) }, quit)
		quit()
	} else {
		<-quitCh
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = adminSrv.Shutdown(ctx)
	_ = apiSrv.Shutdown(ctx)
	log.Printf("小幫手結束")
}

// alreadyRunning 佔住連接埠的是不是另一個小幫手
func alreadyRunning(url string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url + "admin/api/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var s admin.Status
	return json.NewDecoder(resp.Body).Decode(&s) == nil && s.App == admin.AppID
}

func trayLabels() tray.Labels {
	if platform.Chinese() {
		return tray.Labels{Tooltip: "Local AI 小幫手", Open: "打開小幫手", Quit: "結束小幫手"}
	}
	return tray.Labels{Tooltip: "Local AI Helper", Open: "Open Helper", Quit: "Quit Helper"}
}

// setupLog 記錄寫到設定資料夾的 helper.log（Windows 版沒有命令列視窗）；超過 5 MB 就從頭寫
func setupLog(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	path := filepath.Join(dir, "helper.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}

func fatal(err error) {
	log.Printf("無法啟動：%v", err)
	os.Exit(1)
}
