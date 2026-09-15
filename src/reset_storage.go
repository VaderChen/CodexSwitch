package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var resetKeyMu sync.Mutex

// 一張重置券永遠沿用同一請求 ID，包括回應遺失、重試與程式重啟。
func resetRequestKey(dir, accountID, creditID string) (string, error) {
	resetKeyMu.Lock()
	defer resetKeyMu.Unlock()
	if accountID == "" || strings.TrimSpace(creditID) == "" {
		return "", errors.New("無效的重置券")
	}
	sum := sha256.Sum256([]byte(accountID + "\x00" + creditID))
	dir = filepath.Join(dir, "reset-requests")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:]))
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != 36 || data[8] != '-' || data[13] != '-' || data[18] != '-' || data[23] != '-' {
			return "", errors.New("重置請求紀錄不完整，已停止送出以避免重複扣除")
		}
		if _, err = hex.DecodeString(strings.ReplaceAll(string(data), "-", "")); err != nil {
			return "", err
		}
		return string(data), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	key := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err = f.WriteString(key); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	return key, nil
}
