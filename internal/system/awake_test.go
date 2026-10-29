package system

import (
	"errors"
	"testing"
	"time"
)

type fakeInhibitor struct {
	on    bool
	fails int
	calls int
}

func (f *fakeInhibitor) Start() error {
	f.calls++
	if f.fails > 0 {
		f.fails--
		return errors.New("denied")
	}
	f.on = true
	return nil
}
func (f *fakeInhibitor) Stop() { f.on = false }

func TestAwakeHoldsWhileUsedAndLingers(t *testing.T) {
	now := time.Unix(1000, 0)
	inh := &fakeInhibitor{}
	a := &Awake{inh: inh, Linger: 10 * time.Minute, now: func() time.Time { return now }}
	release := a.Hold()
	if !inh.on || !a.Status().Active {
		t.Fatal("開始使用就擋住睡眠")
	}
	// 回答很長：一直擋著
	now = now.Add(30 * time.Minute)
	a.Check()
	if !inh.on {
		t.Fatal("還在用不能放開")
	}
	release()
	release() // 呼叫兩次不會算錯
	now = now.Add(9 * time.Minute)
	a.Check()
	if !inh.on {
		t.Fatal("剛用完還要撐一下（使用者在看回答）")
	}
	now = now.Add(2 * time.Minute)
	a.Check()
	if inh.on || a.Status().Active {
		t.Fatal("閒置夠久就放開")
	}
}

func TestAwakeReportsAndRetriesFailures(t *testing.T) {
	inh := &fakeInhibitor{fails: 1}
	a := &Awake{inh: inh}
	release := a.Hold()
	if st := a.Status(); st.Active || st.Error != "denied" {
		t.Fatalf("%+v", st)
	}
	a.Check()
	if st := a.Status(); !st.Active || st.Error != "" {
		t.Fatalf("下次再試：%+v", st)
	}
	release()
	a.Stop()
	if inh.on {
		t.Fatal("小幫手結束要放開")
	}
	var nilAwake *Awake
	nilAwake.Hold()() // 沒有設定時什麼都不做
}
