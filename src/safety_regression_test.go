package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSafetyRegressionEmptyStoreMustReturnStoreOrError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_SWITCH_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := newAccountStore()
	if s == nil && err == nil {
		t.Fatal("empty accounts.json returns (nil, nil); first account operation will panic")
	}
}

func TestSafetyRegressionStoreMustReplacePermissiveBackup(t *testing.T) {
	s := &accountStore{path: filepath.Join(t.TempDir(), "accounts.json")}
	if err := os.WriteFile(s.path+".bak", []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.path+".bak", 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.upsert(Account{ID: "one", Email: "one@example.invalid", AccessToken: "fixture-secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential file inherits old backup permissions: %04o", info.Mode().Perm())
	}
}

func TestSafetyRegressionStoreMustNotFollowBackupSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "unrelated.txt")
	if err := os.WriteFile(victim, []byte("must survive"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &accountStore{path: filepath.Join(dir, "accounts.json")}
	if err := os.Symlink(victim, s.path+".bak"); err != nil {
		t.Fatal(err)
	}
	err := s.upsert(Account{ID: "one", Email: "one@example.invalid", AccessToken: "fixture-secret"})
	actual, readErr := os.ReadFile(victim)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(actual) != "must survive" {
		t.Fatalf("save overwrote unrelated symlink target; returned error: %v", err)
	}
}

func TestSafetyRegressionDataDirectoryIsExclusive(t *testing.T) {
	t.Setenv("CODEX_SWITCH_DATA_DIR", t.TempDir())
	first, err := newAccountStore()
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	if err := first.upsert(Account{ID: "one", Email: "one@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	if second, err := newAccountStore(); err == nil {
		second.close()
		t.Fatal("second instance must not acquire the same data directory")
	}
	if err := first.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAccountStore()
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	if len(reopened.list()) != 1 {
		t.Fatal("account did not survive reopening")
	}
}

func TestSafetyRegressionCaseAliasesMustSelectRunningInstance(t *testing.T) {
	root := t.TempDir()
	home, data := filepath.Join(root, "Home"), filepath.Join(root, "Data")
	for _, p := range []string{home, data} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	aliasHome, aliasData := filepath.Join(root, "home"), filepath.Join(root, "data")
	a, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(aliasHome)
	if err != nil || !os.SameFile(a, b) {
		t.Skip("requires case-insensitive filesystem")
	}
	home, err = canonicalProcessPath(home)
	if err != nil {
		t.Fatal(err)
	}
	data, err = canonicalProcessPath(data)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := selectCodexInstances([]codexInstance{{PID: 1234, Started: 1, Home: home, Data: data}}, aliasHome, aliasData)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatal("same physical CODEX_HOME and USER_DATA_DIR select zero processes when path case differs")
	}
}

func TestSafetyRegressionNewDuplicateTransactionTargetMustBeRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active-profile.json")
	err := replaceFiles([]fileUpdate{{path, []byte("export CODEX_HOME='/fixture'\n")}, {path, []byte(`{"auth_path":"/fixture/auth.json"}`)}})
	if err == nil {
		t.Fatal("duplicate new paths commit successfully, silently replacing environment file with profile JSON")
	}
}

func TestSafetyRegressionLiteralTildeDirectoryMustRemainLiteral(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := normalizeDirectory("~/codex-review-profile/~")
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(home, "codex-review-profile", "~")
	if actual != expected {
		t.Fatalf("literal final tilde discarded: got %q want %q", actual, expected)
	}
}

func TestSafetyRegressionTransactionNormalPath(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "auth.json"), filepath.Join(dir, "environment")
	if err := os.WriteFile(first, []byte(`{"old":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFiles([]fileUpdate{{first, []byte(`{"new":true}`)}, {second, []byte("fixture")}}); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]bool
	if err := json.Unmarshal(actual, &data); err != nil || !data["new"] {
		t.Fatal("transaction did not commit")
	}
	if _, err := os.Stat(first + ".bak"); !os.IsNotExist(err) {
		t.Fatal("successful transaction left a backup")
	}
}
