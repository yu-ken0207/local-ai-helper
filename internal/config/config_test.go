package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCreatesDefaultsAndKeepsID(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	if len(c.ID) != 32 || c.Name == "" || c.APIPort != DefaultAPIPort || c.AdminPort != DefaultAdminPort || c.Devices == nil {
		t.Fatalf("預設值不對：%+v", c)
	}
	// 重開還是同一個識別碼（手機靠它認電腦）
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Get().ID != c.ID {
		t.Fatal("重開後識別碼變了")
	}
}

func TestUpdateSavesAndGetReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	now := time.Now()
	if err := s.Update(func(c *Config) {
		c.Name = "書房電腦"
		c.Devices = append(c.Devices, Device{ID: "d1", Name: "iPhone", TokenHash: "h", PairedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	got.Devices[0].Name = "改掉複本"
	if s.Get().Devices[0].Name != "iPhone" {
		t.Fatal("Get 應該回傳複本")
	}
	again, _ := Open(dir)
	if c := again.Get(); c.Name != "書房電腦" || len(c.Devices) != 1 || c.Devices[0].Name != "iPhone" {
		t.Fatalf("沒有存起來：%+v", c)
	}
}

func TestOldConfigGetsMissingFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"id":"abc","name":"舊電腦"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c := s.Get(); c.ID != "abc" || c.Name != "舊電腦" || c.APIPort != DefaultAPIPort {
		t.Fatalf("舊設定沒補齊：%+v", c)
	}
}

func TestBrokenConfigIsAnError(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, fileName), []byte(`{壞掉`), 0o600)
	if _, err := Open(dir); err == nil {
		t.Fatal("壞掉的設定檔應該回報錯誤，不能默默蓋掉")
	}
}
