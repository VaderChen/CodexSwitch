//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// 鎖住資料目錄直到 App 結束；保留鎖檔，避免刪除後兩個 inode 被同時鎖住。
func lockAccountStore(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, ".accounts.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fmt.Errorf("無法鎖定帳號資料：%w", err)
	}
	fail := func(err error) (*os.File, error) { _ = f.Close(); return nil, err }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(errors.New("帳號鎖定檔必須是一般檔案"))
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fail(errors.New("另一個 CodexSwitch 正在使用相同帳號資料，請先關閉該實例"))
		}
		return fail(fmt.Errorf("無法鎖定帳號資料：%w", err))
	}
	return f, nil
}
