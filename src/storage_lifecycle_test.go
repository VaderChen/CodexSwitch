package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClosedStoreCannotWriteAfterAnotherInstanceAcquiresLock(t *testing.T) {
	for _, operation := range []string{"upsert", "remove", "settings", "reorder", "import"} {
		t.Run(operation, func(t *testing.T) {
			t.Setenv("CODEX_SWITCH_DATA_DIR", t.TempDir())
			old, err := newAccountStore()
			if err != nil {
				t.Fatal(err)
			}
			defer old.close()
			if err = old.upsert(Account{ID: "one", Email: "one@example.invalid"}); err != nil {
				t.Fatal(err)
			}
			if err = old.close(); err != nil {
				t.Fatal(err)
			}
			current, err := newAccountStore()
			if err != nil {
				t.Fatal(err)
			}
			defer current.close()
			if err = current.upsert(Account{ID: "two", Email: "two@example.invalid"}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(current.path)
			if err != nil {
				t.Fatal(err)
			}
			memory := old.list()
			switch operation {
			case "upsert":
				err = old.upsert(Account{ID: "three", Email: "three@example.invalid"})
			case "remove":
				err = old.remove("one")
			case "settings":
				err = old.saveSettings("one", t.TempDir(), t.TempDir())
			case "reorder":
				err = old.reorder([]string{"one"})
			case "import":
				err = old.importAccounts(nil)
			}
			if err == nil {
				t.Fatal("closed store wrote without owning the directory lock")
			}
			after, readErr := os.ReadFile(current.path)
			if readErr != nil || string(before) != string(after) || !reflect.DeepEqual(memory, old.list()) {
				t.Fatal("closed store changed disk or memory")
			}
		})
	}
}

func TestExportCannotReplaceLiveStoreOrLock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_SWITCH_DATA_DIR", dir)
	s, err := newAccountStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if err = s.upsert(Account{ID: "one", Email: "one@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err = os.WriteFile(filepath.Join(bin, "osascript"), []byte("#!/bin/sh\nprintf '%s\\n' \"$CODEX_SWITCH_TEST_EXPORT_PATH\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{s.path, filepath.Join(dir, ".accounts.lock"), filepath.Join(alias, ".accounts.lock")} {
		t.Run(filepath.Base(filepath.Dir(path))+"-"+filepath.Base(path), func(t *testing.T) {
			t.Setenv("CODEX_SWITCH_TEST_EXPORT_PATH", path)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.exportFile(); err == nil {
				t.Fatal("export overwrote live account data or replaced the locked inode")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatal("rejected export modified protected file")
			}
			if other, err := newAccountStore(); err == nil {
				other.close()
				t.Fatal("export broke directory exclusivity")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "export.json")
	t.Setenv("CODEX_SWITCH_TEST_EXPORT_PATH", path)
	if err = s.exportFile(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("normal private export failed")
	}
}

func TestStoreOverrideKeepsLockAndDataInSamePhysicalDirectory(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "target", "child")
	if err := os.MkdirAll(actual, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	raw := link + "/../store"
	t.Setenv("CODEX_SWITCH_DATA_DIR", raw)
	s, err := newAccountStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if err := s.upsert(Account{ID: "one", Email: "one@example.invalid"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"accounts.json", ".accounts.lock"} {
		if _, err := os.Stat(filepath.Join(root, "target", "store", name)); err != nil {
			t.Fatalf("%s not in physical directory: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(root, "store", name)); !os.IsNotExist(err) {
			t.Fatalf("%s written to lexical parent", name)
		}
	}
}

func TestOutputPathResolvesParentWithoutFollowingFinalSymlink(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "target", "child")
	if err := os.MkdirAll(actual, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "target", "export.json")
	victim := filepath.Join(root, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, output); err != nil {
		t.Fatal(err)
	}
	resolved, err := absoluteOutputPath(link + "/../export.json")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(resolved)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("output path followed final symlink")
	}
	if err := replaceFiles([]fileUpdate{{resolved, []byte("changed")}}); err == nil {
		t.Fatal("transaction accepted final symlink")
	}
	s := &accountStore{path: filepath.Join(root, "accounts.json")}
	if err := s.exportTo(link + "/../export.json"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(victim)
	if err != nil || string(data) != "keep" {
		t.Fatal("export modified final symlink target")
	}
	info, err = os.Lstat(output)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("export did not replace selected output")
	}
}
