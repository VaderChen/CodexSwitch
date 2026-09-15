package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func reviewAccount(email string) Account {
	payload, _ := json.Marshal(map[string]string{"email": email})
	auth := codexAuth{Mode: "chatgpt"}
	auth.Tokens.IDToken = "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	auth.Tokens.AccessToken = "fixture-access"
	auth.Tokens.RefreshToken = "fixture-refresh"
	data, _ := json.Marshal(auth)
	return Account{ID: "');window.injected=true;//", Email: email, Auth: data}
}
func TestImportValidatesAndCommitsTogether(t *testing.T) {
	s := &accountStore{path: filepath.Join(t.TempDir(), "accounts.json")}
	a := reviewAccount("fixture@example.com")
	if err := s.importAccounts([]Account{a}); err != nil {
		t.Fatal(err)
	}
	got := s.list()[0]
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(got.ID) || got.AccessToken != "fixture-access" {
		t.Fatal("未由憑證重建 ID/token")
	}
	before, _ := os.ReadFile(s.path)
	if err := s.importAccounts([]Account{reviewAccount("next@example.com"), {Email: "bad@example.com", Auth: json.RawMessage(`{}`)}}); err == nil {
		t.Fatal("必須拒絕不完整的匯入")
	}
	after, _ := os.ReadFile(s.path)
	if string(before) != string(after) || len(s.list()) != 1 {
		t.Fatal("不可部分匯入")
	}
	s.path = filepath.Join(t.TempDir(), "missing", "accounts.json")
	if err := s.importAccounts([]Account{reviewAccount("next@example.com")}); err == nil {
		t.Fatal("必須回報寫入失敗")
	}
	if len(s.list()) != 1 {
		t.Fatal("寫入失敗必須恢復記憶體資料")
	}
}
func TestResetRequestSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := resetRequestKey(dir, "account", "credit")
	if err != nil {
		t.Fatal(err)
	}
	again, err := resetRequestKey(dir, "account", "credit")
	if err != nil || again != first {
		t.Fatal("重試必須沿用相同 ID")
	}
	other, err := resetRequestKey(dir, "account", "other")
	if err != nil || other == first {
		t.Fatal("不同券不可沿用同一請求")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "reset-requests"))
	for _, entry := range entries {
		info, _ := entry.Info()
		if info.Mode().Perm() != 0600 {
			t.Fatal("請求檔案權限必須為 0600")
		}
	}
}
func TestCodexExecutableOverrideBeforeAnyWrite(t *testing.T) {
	t.Setenv("CODEX_EXE", filepath.Join(t.TempDir(), "missing"))
	if _, err := resolveCodexExecutable(); err == nil {
		t.Fatal("必須拒絕不存在的執行檔")
	}
	s := &accountStore{path: filepath.Join(t.TempDir(), "accounts.json")}
	if _, err := newLoginManager(s).useAccount("invalid"); err == nil {
		t.Fatal("無效執行檔不可套用")
	}
	entries, _ := os.ReadDir(filepath.Dir(s.path))
	if len(entries) != 0 {
		t.Fatal("預檢失敗不可修改資料")
	}
}
func TestCodexBundleExecutableComesFromPlist(t *testing.T) {
	t.Setenv("CODEX_EXE", "")
	app := filepath.Join(t.TempDir(), "Codex.app")
	t.Setenv("CODEX_APP_PATH", app)
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0700)
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.openai.codex</string><key>CFBundleExecutable</key><string>CustomCodex</string></dict></plist>`
	os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0600)
	exe := filepath.Join(app, "Contents", "MacOS", "CustomCodex")
	os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0700)
	got, err := resolveCodexExecutable()
	if err != nil || got != exe {
		t.Fatalf("未使用 bundle 執行檔：%s %v", got, err)
	}
}
