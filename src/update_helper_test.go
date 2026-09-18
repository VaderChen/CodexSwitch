//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateHelperReplacesAndRollsBack(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "launch failure restores original"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "App With Spaces.app")
			work := filepath.Join(dir, "stage")
			os.MkdirAll(target, 0700)
			os.WriteFile(filepath.Join(target, "version"), []byte("old"), 0600)
			os.MkdirAll(filepath.Join(work, "CodexSwitch.app"), 0700)
			os.WriteFile(filepath.Join(work, "CodexSwitch.app", "version"), []byte("new"), 0600)
			opener := filepath.Join(dir, "open-stub")
			body := "#!/bin/sh\ntouch \"$5\"\nexit 0\n"
			if fail {
				body = "#!/bin/sh\n[ \"$(cat \"$2/version\")\" = old ]\n"
			}
			os.WriteFile(opener, []byte(body), 0700)
			script := filepath.Join(dir, "helper.sh")
			os.WriteFile(script, []byte(strings.ReplaceAll(updateHelper, "/usr/bin/open", shellQuote(opener))), 0700)
			err := exec.Command("/bin/sh", script, "99999999", target, work).Run()
			if (err != nil) != fail {
				t.Fatalf("helper result: %v", err)
			}
			b, e := os.ReadFile(filepath.Join(target, "version"))
			if e != nil {
				t.Fatal(e)
			}
			want := "new"
			if fail {
				want = "old"
			}
			if string(b) != want {
				t.Fatalf("expected %s", want)
			}
		})
	}
}

func TestUpdateHelperKeepsBackupWithoutReady(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Example.app")
	work := filepath.Join(dir, "work")
	os.MkdirAll(target, 0700)
	os.WriteFile(filepath.Join(target, "old"), []byte("original"), 0600)
	os.MkdirAll(filepath.Join(work, "CodexSwitch.app"), 0700)
	opener := filepath.Join(dir, "open")
	os.WriteFile(opener, []byte("#!/bin/sh\nexit 0\n"), 0700)
	script := strings.ReplaceAll(updateHelper, "/usr/bin/open", shellQuote(opener))
	script = strings.ReplaceAll(script, "sleep 1", "sleep 0.01")
	path := filepath.Join(dir, "helper")
	os.WriteFile(path, []byte(script), 0700)
	if err := exec.Command("/bin/sh", path, "99999999", target, work).Run(); err == nil {
		t.Fatal("must report missing ready acknowledgment")
	}
	if b, err := os.ReadFile(filepath.Join(target+".update.bak", "old")); err != nil || string(b) != "original" {
		t.Fatal("must preserve working original backup")
	}
}

func TestArchiveOldUpdateBackup(t *testing.T) {
	target := filepath.Join(t.TempDir(), "CodexSwitch.app")
	backup := target + ".update.bak"
	if err := archiveUpdateBackup(target, time.Now()); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(backup, 0700)
	os.WriteFile(filepath.Join(backup, "original"), []byte("restore"), 0600)
	if err := archiveUpdateBackup(target, time.Now()); err == nil {
		t.Fatal("不應移動可能仍在使用中的備份")
	}
	if err := archiveUpdateBackup(target, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("舊備份仍擋住更新")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".codexswitch-backup-*", "CodexSwitch.app", "original"))
	if len(files) != 1 {
		t.Fatal("必須保留舊版供還原")
	}
	data, _ := os.ReadFile(files[0])
	if string(data) != "restore" {
		t.Fatal("備份內容改變")
	}
	if err := archiveUpdateBackup(target, time.Now()); err != nil {
		t.Fatal("再次更新應可通過")
	}
	os.Symlink(filepath.Dir(target), backup)
	if err := archiveUpdateBackup(target, time.Now().Add(2*time.Minute)); err == nil {
		t.Fatal("不可操作備份符號連結")
	}
}
