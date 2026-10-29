package system

import (
	"bytes"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/yu-ken0207/local-ai-helper/internal/platform"
)

//go:embed icon.ico
var appIcon []byte

// InstallLauncher 在「開始」功能表放一個「Local AI 小幫手」捷徑：結束之後一般使用者不用找執行檔就能再打開。
// 建捷徑要用 PowerShell（一秒多），所以執行檔位置沒變、捷徑也還在時就不動
func InstallLauncher(exe string) error {
	local := os.Getenv("LOCALAPPDATA")
	appData := os.Getenv("APPDATA")
	if local == "" || appData == "" {
		return nil
	}
	dir := filepath.Join(local, "local-ai-helper")
	icon := filepath.Join(dir, "icon.ico")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if old, err := os.ReadFile(icon); err != nil || !bytes.Equal(old, appIcon) {
		if err := os.WriteFile(icon, appIcon, 0o644); err != nil {
			return err
		}
	}
	name := "Local AI Helper.lnk"
	if platform.Chinese() {
		name = "Local AI 小幫手.lnk"
	}
	lnk := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", name)
	marker := filepath.Join(dir, "launcher.txt")
	if done, err := os.ReadFile(marker); err == nil && string(done) == exe+"\n"+lnk {
		if _, err := os.Stat(lnk); err == nil {
			return nil
		}
	}
	// 路徑用環境變數傳進去，不用處理引號
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:LAI_LNK); $s.TargetPath = $env:LAI_EXE; $s.WorkingDirectory = $env:LAI_DIR; $s.IconLocation = $env:LAI_ICON; $s.Description = 'Local AI Helper'; $s.Save()`)
	cmd.Env = append(os.Environ(), "LAI_LNK="+lnk, "LAI_EXE="+exe, "LAI_DIR="+filepath.Dir(exe), "LAI_ICON="+icon)
	platform.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return &launcherError{string(out)}
	}
	return os.WriteFile(marker, []byte(exe+"\n"+lnk), 0o644)
}

type launcherError struct{ out string }

func (e *launcherError) Error() string { return "create Start menu shortcut: " + e.out }
