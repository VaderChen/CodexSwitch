package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreLockAcrossProcesses(t *testing.T) {
	t.Setenv("CODEX_SWITCH_DATA_DIR", t.TempDir())
	first, err := newAccountStore()
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	if err := first.upsert(Account{ID: "one", Email: "one@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	probe := func(mode string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestStoreLockProbe$")
		cmd.Env = append(os.Environ(), "CODEX_SWITCH_LOCK_PROBE="+mode)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child %s: %v\n%s", mode, err, out)
		}
	}
	probe("blocked")
	if err := first.close(); err != nil {
		t.Fatal(err)
	}
	probe("available")
	reopened, err := newAccountStore()
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	if len(reopened.list()) != 2 {
		t.Fatal("serialized processes lost accounts")
	}
}

func TestStoreLockProbe(t *testing.T) {
	mode := os.Getenv("CODEX_SWITCH_LOCK_PROBE")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	s, err := newAccountStore()
	if mode == "blocked" {
		if err == nil {
			s.close()
			t.Fatal("child acquired another process's store")
		}
		if !strings.Contains(err.Error(), "另一個 CodexSwitch") {
			t.Fatal(err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if len(s.list()) != 1 {
		t.Fatal("existing account missing")
	}
	if err := s.upsert(Account{ID: "two", Email: "two@example.invalid"}); err != nil {
		t.Fatal(err)
	}
}

func TestStoreInitializationFailureReleasesLock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_SWITCH_DATA_DIR", dir)
	path := filepath.Join(dir, "accounts.json")
	for _, content := range []string{"", " \n", "invalid JSON"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if s, err := newAccountStore(); err == nil {
			s.close()
			t.Fatal("invalid store accepted")
		}
		if err := os.WriteFile(path, []byte("[]"), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := newAccountStore()
		if err != nil {
			t.Fatal("failed initialization leaked lock:", err)
		}
		s.close()
	}
}

func TestStoreLockRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_SWITCH_DATA_DIR", dir)
	target := filepath.Join(dir, "unrelated")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".accounts.lock")); err != nil {
		t.Fatal(err)
	}
	if s, err := newAccountStore(); err == nil {
		s.close()
		t.Fatal("symlink lock accepted")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "keep" {
		t.Fatal("lock target changed")
	}
}
