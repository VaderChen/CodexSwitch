package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountSettingsDefaultsAndPreservation(t *testing.T) {
	s := &accountStore{path: filepath.Join(t.TempDir(), "accounts.json"), accounts: []Account{{ID: "a", Email: "a@example.com"}}}
	if err := s.saveSettings("a", "  ", ""); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	a := s.list()[0]
	if a.CodexHome != filepath.Join(home, ".codex") || a.UserDataDir != filepath.Join(home, "Library", "Application Support", "Codex") {
		t.Fatal("預設值未填入")
	}
	custom := filepath.Join(t.TempDir(), "custom")
	if err := s.saveSettings("a", custom, custom); err != nil {
		t.Fatal(err)
	}
	if err := s.upsert(Account{ID: "a", Email: a.Email}); err != nil {
		t.Fatal(err)
	}
	a = s.list()[0]
	if a.CodexHome != custom || a.UserDataDir != custom {
		t.Fatal("更新憑證覆蓋了設定")
	}
	if err := s.saveSettings("a", "relative", custom); err == nil {
		t.Fatal("應拒絕相對路徑")
	}
	if s.list()[0].CodexHome != custom {
		t.Fatal("錯誤設定不應改動原資料")
	}
}
