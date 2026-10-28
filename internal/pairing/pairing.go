// Package pairing 管手機配對：開一個 5 分鐘的配對時段，手機用 6 位數配對碼換到權杖；之後每個請求都要帶權杖。
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
)

const (
	// WindowDuration 按「配對新手機」後可以配對的時間
	WindowDuration = 5 * time.Minute
	// MaxFailures 一個時段裡打錯幾次就關掉（6 位數只有一百萬種，不能讓人一直猜）
	MaxFailures = 5
	// seenInterval 「最近使用」多久才寫一次設定檔，免得每個請求都寫檔
	seenInterval = time.Minute
)

var (
	ErrNotPairing = errors.New("not pairing")
	ErrBadCode    = errors.New("wrong code")
	ErrTooMany    = errors.New("too many attempts")
)

// Window 目前的配對時段
type Window struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
	failures  int
}

type Manager struct {
	mu     sync.Mutex
	store  *config.Store
	now    func() time.Time
	window *Window
	// lastPaired 剛配對成功的手機（管理頁用來顯示「已配對」）
	lastPaired *config.Device
}

func New(store *config.Store) *Manager {
	return &Manager{store: store, now: time.Now}
}

// SetClock 測試用
func (m *Manager) SetClock(now func() time.Time) { m.now = now }

// Start 開一個新的配對時段（舊的作廢）
func (m *Manager) Start() Window {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.window = &Window{Code: randomCode(), ExpiresAt: m.now().Add(WindowDuration)}
	m.lastPaired = nil
	return *m.window
}

// Stop 取消配對時段
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.window = nil
}

// Active 目前有沒有在配對（過期就當作沒有）
func (m *Manager) Active() (Window, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w := m.activeLocked(); w != nil {
		return *w, true
	}
	return Window{}, false
}

func (m *Manager) activeLocked() *Window {
	if m.window != nil && m.now().After(m.window.ExpiresAt) {
		m.window = nil
	}
	return m.window
}

// LastPaired 這次配對時段裡配對成功的手機
func (m *Manager) LastPaired() *config.Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastPaired == nil {
		return nil
	}
	d := *m.lastPaired
	return &d
}

// Pair 手機送來配對碼；對了就發權杖（只回傳這一次，小幫手只存雜湊）。一個時段只配一支手機。
func (m *Manager) Pair(code, deviceName string) (string, config.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.activeLocked()
	if w == nil {
		return "", config.Device{}, ErrNotPairing
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(code)), []byte(w.Code)) != 1 {
		w.failures++
		if w.failures >= MaxFailures {
			m.window = nil
			return "", config.Device{}, ErrTooMany
		}
		return "", config.Device{}, ErrBadCode
	}
	token := randomToken()
	dev := config.Device{ID: config.NewID(8), Name: cleanName(deviceName), TokenHash: HashToken(token), PairedAt: m.now()}
	if err := m.store.Update(func(c *config.Config) { c.Devices = append(c.Devices, dev) }); err != nil {
		return "", config.Device{}, err
	}
	m.window = nil
	m.lastPaired = &dev
	return token, dev, nil
}

// Authenticate 檢查權杖；對了回傳是哪支手機，順便記下最近使用時間
func (m *Manager) Authenticate(token string) (config.Device, bool) {
	if token == "" {
		return config.Device{}, false
	}
	hash := HashToken(token)
	for _, d := range m.store.Get().Devices {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(d.TokenHash)) == 1 {
			now := m.now()
			if d.LastSeenAt == nil || now.Sub(*d.LastSeenAt) > seenInterval {
				_ = m.store.Update(func(c *config.Config) {
					for i := range c.Devices {
						if c.Devices[i].ID == d.ID {
							c.Devices[i].LastSeenAt = &now
						}
					}
				})
			}
			return d, true
		}
	}
	return config.Device{}, false
}

// Revoke 移除一支手機；它的權杖馬上失效
func (m *Manager) Revoke(id string) bool {
	found := false
	_ = m.store.Update(func(c *config.Config) {
		kept := c.Devices[:0]
		for _, d := range c.Devices {
			if d.ID == id {
				found = true
				continue
			}
			kept = append(kept, d)
		}
		c.Devices = kept
	})
	return found
}

// HashToken 權杖的雜湊（設定檔裡只存這個）
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic(err)
	}
	s := n.String()
	return strings.Repeat("0", 6-len(s)) + s
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// cleanName 手機名稱：去掉控制字元，最多 40 字；沒給就叫「手機」
func cleanName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	runes := []rune(b.String())
	if len(runes) > 40 {
		runes = runes[:40]
	}
	if len(runes) == 0 {
		return "iPhone"
	}
	return string(runes)
}
