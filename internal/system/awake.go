package system

import (
	"context"
	"sync"
	"time"
)

// Inhibitor 叫作業系統暫時不要睡（各平台實作；測試可以換成假的）
type Inhibitor interface {
	Start() error
	Stop()
}

// Awake 手機在用（聊天、載入模型、下載）時不讓電腦睡著；用完再等一段時間（使用者可能在看回答、打下一句）才放開。
// 螢幕照常可以關，只是電腦不進入睡眠。
type Awake struct {
	// Linger 最後一次使用後再撐多久（預設 10 分鐘）
	Linger time.Duration

	mu    sync.Mutex
	holds int
	last  time.Time
	on    bool
	err   string
	inh   Inhibitor
	now   func() time.Time
}

// NewAwake 用這個平台的方式
func NewAwake() *Awake { return &Awake{inh: newInhibitor()} }

// NewAwakeWith 指定擋睡眠的方式（測試用）
func NewAwakeWith(inh Inhibitor) *Awake { return &Awake{inh: inh} }

func (a *Awake) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func (a *Awake) linger() time.Duration {
	if a.Linger > 0 {
		return a.Linger
	}
	return 10 * time.Minute
}

// Hold 開始使用（聊天、下載）；回傳的函式在用完時呼叫
func (a *Awake) Hold() func() {
	if a == nil {
		return func() {}
	}
	a.mu.Lock()
	a.holds++
	a.last = a.clock()
	a.applyLocked()
	a.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.holds--
			a.last = a.clock()
			a.mu.Unlock()
		})
	}
}

// Run 每 30 秒看一次要不要放開
func (a *Awake) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			a.Stop()
			return
		case <-t.C:
			a.Check()
		}
	}
}

// Check 照目前的使用情況開或關
func (a *Awake) Check() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyLocked()
}

func (a *Awake) applyLocked() {
	want := a.holds > 0 || (!a.last.IsZero() && a.clock().Sub(a.last) < a.linger())
	if want == a.on || a.inh == nil {
		return
	}
	if want {
		if err := a.inh.Start(); err != nil {
			a.err = err.Error()
			return
		}
		a.err = ""
	} else {
		a.inh.Stop()
	}
	a.on = want
}

// Stop 小幫手結束時放開
func (a *Awake) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.on && a.inh != nil {
		a.inh.Stop()
	}
	a.on = false
}

// AwakeStatus 管理頁顯示
type AwakeStatus struct {
	// Active 現在擋著不讓電腦睡
	Active bool   `json:"active"`
	Error  string `json:"error,omitempty"`
}

func (a *Awake) Status() AwakeStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := AwakeStatus{Active: a.on, Error: a.err}
	// 擋睡眠的程式自己結束了（Linux 沒有權限時）
	if e, ok := a.inh.(interface{ Err() string }); ok && a.on {
		if msg := e.Err(); msg != "" {
			st.Active, st.Error = false, msg
		}
	}
	return st
}
