package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoreFailureRestoresMemory(t *testing.T) {
	for _, operation := range []string{"remove", "insert", "update"} {
		t.Run(operation, func(t *testing.T) {
			original := []Account{{ID: "a", Email: "a@example.com"}, {ID: "b", Email: "b@example.com"}}
			s := &accountStore{path: filepath.Join(t.TempDir(), "missing", "accounts.json"), accounts: append([]Account(nil), original...)}
			var err error
			switch operation {
			case "remove":
				err = s.remove("a")
			case "insert":
				err = s.upsert(Account{ID: "c", Email: "c@example.com"})
			case "update":
				err = s.upsert(Account{ID: "a", Email: "a@example.com", AccessToken: "changed"})
			}
			if err == nil || !reflect.DeepEqual(s.list(), original) {
				t.Fatal("failed write must preserve original accounts")
			}
		})
	}
}
func TestExportReplacesPermissionsAndDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	os.WriteFile(path, []byte("old"), 0644)
	if err := writePrivateFile(path, []byte("fixture")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("export is not private")
	}
	link := filepath.Join(dir, "link.json")
	os.Symlink(path, link)
	if err := writePrivateFile(link, []byte("other")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "fixture" {
		t.Fatal("symlink target was overwritten")
	}
	info, _ = os.Lstat(link)
	if !info.Mode().IsRegular() {
		t.Fatal("must replace symlink with private file")
	}
}
