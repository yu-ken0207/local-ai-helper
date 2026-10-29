package system

import (
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// executionState Windows 的 SetThreadExecutionState 只對呼叫它的執行緒有效，所以用一個固定的執行緒來開關
type executionState struct {
	req chan bool
	ack chan error
}

func newInhibitor() Inhibitor {
	e := &executionState{req: make(chan bool), ack: make(chan error)}
	go func() {
		runtime.LockOSThread()
		for on := range e.req {
			flags := uintptr(esContinuous)
			if on {
				flags |= esSystemRequired
			}
			// 失敗時回傳 0
			if r, _, err := setThreadExecutionState.Call(flags); r == 0 {
				e.ack <- err
			} else {
				e.ack <- nil
			}
		}
	}()
	return e
}

func (e *executionState) Start() error {
	e.req <- true
	return <-e.ack
}

func (e *executionState) Stop() {
	e.req <- false
	<-e.ack
}
