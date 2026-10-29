package system

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// 使用者自己的開機啟動項目（HKCU\...\Run），不用管理員權限；工作管理員的「啟動」分頁看得到也能停用
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValue = "LocalAIHelper"

// SetAutostart 打開或關掉開機自動啟動；打開時每次都重寫（執行檔搬了位置也跟著改）
func SetAutostart(enabled bool, exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	return k.SetStringValue(runValue, `"`+exe+`" `+autostartArgs)
}

// AutostartEnabled 現在有沒有設定開機自動啟動
func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}
