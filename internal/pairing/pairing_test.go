package pairing

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
)

func newManager(t *testing.T) (*Manager, *config.Store, *time.Time) {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	m := New(store)
	m.SetClock(func() time.Time { return now })
	return m, store, &now
}

func TestPairWithRightCode(t *testing.T) {
	m, store, _ := newManager(t)
	if _, _, err := m.Pair("123456", "x"); !errors.Is(err, ErrNotPairing) {
		t.Fatalf("沒開配對時段應該不能配對：%v", err)
	}
	w := m.Start()
	if len(w.Code) != 6 || strings.Trim(w.Code, "0123456789") != "" {
		t.Fatalf("配對碼要是 6 位數字：%q", w.Code)
	}
	token, dev, err := m.Pair(" "+w.Code+" ", "Ken 的 iPhone")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 40 || dev.Name != "Ken 的 iPhone" {
		t.Fatalf("權杖或名稱不對：%q %+v", token, dev)
	}
	// 設定檔只存雜湊
	saved := store.Get().Devices
	if len(saved) != 1 || saved[0].TokenHash != HashToken(token) || strings.Contains(saved[0].TokenHash, token) {
		t.Fatalf("存的不是雜湊：%+v", saved)
	}
	// 一個時段只配一支，配完就關
	if _, ok := m.Active(); ok {
		t.Fatal("配對成功後時段要關掉")
	}
	if d := m.LastPaired(); d == nil || d.ID != dev.ID {
		t.Fatal("要記得剛配對的手機")
	}
	if got, ok := m.Authenticate(token); !ok || got.ID != dev.ID {
		t.Fatal("權杖應該有效")
	}
}

func TestWrongCodesCloseWindow(t *testing.T) {
	m, _, _ := newManager(t)
	w := m.Start()
	wrong := "000000"
	if w.Code == wrong {
		wrong = "111111"
	}
	for i := 1; i < MaxFailures; i++ {
		if _, _, err := m.Pair(wrong, "x"); !errors.Is(err, ErrBadCode) {
			t.Fatalf("第 %d 次打錯：%v", i, err)
		}
	}
	if _, _, err := m.Pair(wrong, "x"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("打錯 %d 次要關掉時段：%v", MaxFailures, err)
	}
	if _, _, err := m.Pair(w.Code, "x"); !errors.Is(err, ErrNotPairing) {
		t.Fatalf("關掉之後對的碼也不行：%v", err)
	}
}

func TestWindowExpires(t *testing.T) {
	m, _, now := newManager(t)
	w := m.Start()
	*now = now.Add(WindowDuration + time.Second)
	if _, ok := m.Active(); ok {
		t.Fatal("5 分鐘後時段要過期")
	}
	if _, _, err := m.Pair(w.Code, "x"); !errors.Is(err, ErrNotPairing) {
		t.Fatalf("過期後不能配對：%v", err)
	}
}

func TestStartAgainReplacesCode(t *testing.T) {
	m, _, _ := newManager(t)
	first := m.Start()
	var second Window
	for i := 0; i < 5; i++ { // 極少數會抽到一樣的碼
		if second = m.Start(); second.Code != first.Code {
			break
		}
	}
	if _, _, err := m.Pair(first.Code, "x"); err == nil && first.Code != second.Code {
		t.Fatal("舊的配對碼要作廢")
	}
}

func TestAuthenticateAndRevoke(t *testing.T) {
	m, store, now := newManager(t)
	token, dev, _ := m.Pair(m.Start().Code, "")
	if dev.Name != "iPhone" {
		t.Fatalf("沒給名稱要用預設：%q", dev.Name)
	}
	if _, ok := m.Authenticate("亂猜的權杖"); ok {
		t.Fatal("亂猜的權杖不能過")
	}
	if _, ok := m.Authenticate(""); ok {
		t.Fatal("空權杖不能過")
	}
	m.Authenticate(token)
	first := store.Get().Devices[0].LastSeenAt
	if first == nil {
		t.Fatal("要記下最近使用時間")
	}
	// 一分鐘內不重寫
	*now = now.Add(30 * time.Second)
	m.Authenticate(token)
	if !store.Get().Devices[0].LastSeenAt.Equal(*first) {
		t.Fatal("一分鐘內不用重寫最近使用時間")
	}
	if !m.Revoke(dev.ID) || m.Revoke(dev.ID) {
		t.Fatal("移除一次成功，第二次找不到")
	}
	if _, ok := m.Authenticate(token); ok {
		t.Fatal("移除後權杖要失效")
	}
}

func TestCleanName(t *testing.T) {
	if got := cleanName("  a\x00b\nc  "); got != "abc" {
		t.Fatalf("控制字元要去掉：%q", got)
	}
	if got := cleanName(strings.Repeat("長", 50)); len([]rune(got)) != 40 {
		t.Fatalf("最多 40 字：%d", len([]rune(got)))
	}
}

func TestPayloadAndQR(t *testing.T) {
	p := Payload("abc", "書房 電腦", 11480, "012345", []string{"192.168.1.20", "100.100.200.17"})
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "localai" || u.Host != "pair" {
		t.Fatalf("格式不對：%s", p)
	}
	q := u.Query()
	if q.Get("id") != "abc" || q.Get("name") != "書房 電腦" || q.Get("port") != "11480" || q.Get("code") != "012345" || len(q["ip"]) != 2 {
		t.Fatalf("內容不對：%v", q)
	}
	img, err := QRDataURL(p)
	if err != nil || !strings.HasPrefix(img, "data:image/png;base64,") {
		t.Fatalf("QR code 圖片不對：%v", err)
	}
}
