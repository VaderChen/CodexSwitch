package main

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProcessArgsAndDirectories(t *testing.T) {
	args := []string{"/Applications/Codex.app/Contents/MacOS/Codex", "--user-data-dir", "/Users/test/Profile With Spaces"}
	raw := make([]byte, 4)
	binary.NativeEndian.PutUint32(raw, uint32(len(args)))
	raw = append(raw, []byte(args[0]+"\x00\x00")...)
	for _, a := range args {
		raw = append(raw, []byte(a+"\x00")...)
	}
	raw = append(raw, []byte("HOME=/Users/test\x00CODEX_HOME=/Users/test/codex one\x00SECRET=never-return\x00\x00")...)
	a, e, err := parseProcessArgs(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e["SECRET"]; ok {
		t.Fatal("不應保留其他環境變數")
	}
	h, d, err := processDirectories(a, e)
	if err != nil || h != "/Users/test/codex one" || d != args[2] {
		t.Fatalf("不正確目錄 %q %q %v", h, d, err)
	}
	h, d, err = processDirectories([]string{"Codex"}, map[string]string{"HOME": "/Users/test"})
	if err != nil || h != "/Users/test/.codex" || d != "/Users/test/Library/Application Support/Codex" {
		t.Fatal("預設目錄錯誤")
	}
	if _, _, err = processDirectories([]string{"Codex"}, map[string]string{"CODEX_HOME": "relative"}); err == nil {
		t.Fatal("不明相對路徑應停止")
	}
	if _, _, err = parseProcessArgs([]byte{1, 2, 3}); err == nil {
		t.Fatal("截斷資料未拒絕")
	}
}
func TestTargetedClose(t *testing.T) {
	a := codexInstance{PID: 11, Started: 1, Home: "/profiles/a", Data: "/data/a"}
	b := codexInstance{PID: 22, Started: 2, Home: "/profiles/ab", Data: "/data/ab"}
	list := []codexInstance{a, b}
	var stopped []int
	err := closeTargetCodex(a.Home, a.Data, func() ([]codexInstance, error) { return list, nil }, func(p codexInstance) error { stopped = append(stopped, p.PID); list = []codexInstance{b}; return nil })
	if err != nil || len(stopped) != 1 || stopped[0] != 11 {
		t.Fatalf("關閉錯誤 %v %v", stopped, err)
	}
	b.Data = a.Data
	if _, err := selectCodexInstances([]codexInstance{a, b}, a.Home, a.Data); err == nil {
		t.Fatal("資料目錄衝突未拒絕")
	}
	stopped = nil
	err = closeTargetCodex(a.Home, a.Data, func() ([]codexInstance, error) { return nil, errors.New("無讀取權限") }, func(p codexInstance) error { stopped = append(stopped, p.PID); return nil })
	if err == nil || len(stopped) > 0 {
		t.Fatal("辨識失敗不可關閉程序")
	}
	b.Home = a.Home
	b.Data = "/data/other"
	targets, err := selectCodexInstances([]codexInstance{a, b}, a.Home, a.Data)
	if err != nil || len(targets) != 2 {
		t.Fatal("共用 auth 的程序應一起關閉")
	}
}
func TestCanonicalProcessPath(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	a, err := canonicalProcessPath(filepath.Join(alias, "new"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalProcessPath(filepath.Join(actual, "new"))
	if err != nil || a != b {
		t.Fatal("符號連結不一致")
	}
}

func TestNoMatchingInstanceStartsWithoutClosing(t *testing.T) {
	scans := 0
	err := closeTargetCodex("/profiles/new", "/data/new", func() ([]codexInstance, error) {
		scans++
		return []codexInstance{{PID: 22, Home: "/profiles/other", Data: "/data/other", Started: 0}}, nil
	}, func(codexInstance) error { t.Fatal("不可關閉其他實例"); return nil })
	if err != nil || scans != 1 {
		t.Fatalf("無對應實例應立即允許啟動：%v，掃描 %d 次", err, scans)
	}
}
