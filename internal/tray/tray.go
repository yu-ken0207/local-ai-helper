// Package tray 在系統匣（Windows 右下角、Linux 桌面上方）放小幫手的圖示，可以打開管理頁或結束。
package tray

import (
	_ "embed"
	"runtime"

	"fyne.io/systray"
)

//go:embed icon.png
var iconPNG []byte

//go:embed icon.ico
var iconICO []byte

type Labels struct {
	Tooltip string
	Open    string
	Quit    string
}

// Run 顯示圖示並卡住，直到 Quit 被呼叫；一定要在主執行緒呼叫
func Run(l Labels, onOpen func(), onQuit func()) {
	systray.Run(func() {
		if runtime.GOOS == "windows" {
			systray.SetIcon(iconICO)
		} else {
			systray.SetIcon(iconPNG)
		}
		systray.SetTooltip(l.Tooltip)
		// Windows 點一下圖示就打開管理頁
		systray.SetOnTapped(onOpen)
		open := systray.AddMenuItem(l.Open, "")
		systray.AddSeparator()
		quit := systray.AddMenuItem(l.Quit, "")
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					onOpen()
				case <-quit.ClickedCh:
					onQuit()
					return
				}
			}
		}()
	}, nil)
}

// Quit 收起圖示，讓 Run 返回
func Quit() { systray.Quit() }
