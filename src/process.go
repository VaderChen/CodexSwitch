package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type codexInstance struct {
	PID     int     `json:"pid"`
	Started float64 `json:"started"`
	Home    string
	Data    string
}

// KERN_PROCARGS2：argc、執行檔路徑、補零、argv、環境變數。
func parseProcessArgs(raw []byte) ([]string, map[string]string, error) {
	invalid := errors.New("程序環境資料不完整")
	if len(raw) < 4 {
		return nil, nil, invalid
	}
	argc := int(binary.NativeEndian.Uint32(raw[:4]))
	raw = raw[4:]
	if argc < 1 || argc > 65536 {
		return nil, nil, invalid
	}
	i := bytes.IndexByte(raw, 0)
	if i < 0 {
		return nil, nil, invalid
	}
	raw = raw[i+1:]
	for len(raw) > 0 && raw[0] == 0 {
		raw = raw[1:]
	}
	var args []string
	for j := 0; j < argc; j++ {
		i = bytes.IndexByte(raw, 0)
		if i < 0 {
			return nil, nil, invalid
		}
		args = append(args, string(raw[:i]))
		raw = raw[i+1:]
	}
	env := map[string]string{}
	for _, entry := range bytes.Split(raw, []byte{0}) {
		key, val, ok := strings.Cut(string(entry), "=")
		if ok && (key == "HOME" || key == "CODEX_HOME" || key == "USER_DATA_DIR" || key == "CODEX_USER_DATA_DIR") {
			env[key] = val
		}
	}
	return args, env, nil
}
func canonicalProcessPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("目錄不是絕對路徑")
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	// 未建立的目錄也必須解析既有父目錄的符號連結。
	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	base, err := canonicalProcessPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.Base(path)), nil
}
func processDirectories(args []string, env map[string]string) (string, string, error) {
	home := env["CODEX_HOME"]
	if home == "" {
		if !filepath.IsAbs(env["HOME"]) {
			return "", "", errors.New("無法判定預設 HOME")
		}
		home = filepath.Join(env["HOME"], ".codex")
	}
	data := env["CODEX_USER_DATA_DIR"]
	if data == "" {
		data = env["USER_DATA_DIR"]
	}
	for i := 1; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--user-data-dir=") {
			data = strings.TrimPrefix(args[i], "--user-data-dir=")
			if data == "" {
				return "", "", errors.New("資料目錄參數為空")
			}
		}
		if args[i] == "--user-data-dir" {
			i++
			if i == len(args) {
				return "", "", errors.New("資料目錄參數不完整")
			}
			data = args[i]
		}
	}
	if data == "" {
		if !filepath.IsAbs(env["HOME"]) {
			return "", "", errors.New("無法判定預設 HOME")
		}
		data = filepath.Join(env["HOME"], "Library", "Application Support", "Codex")
	}
	h, err := canonicalProcessPath(home)
	if err != nil {
		return "", "", err
	}
	d, err := canonicalProcessPath(data)
	return h, d, err
}
func selectCodexInstances(list []codexInstance, home, data string) ([]codexInstance, error) {
	h, err := canonicalProcessPath(home)
	if err != nil {
		return nil, err
	}
	d, err := canonicalProcessPath(data)
	if err != nil {
		return nil, err
	}
	var targets []codexInstance
	for _, p := range list {
		if p.Home != h {
			if p.Data == d {
				return nil, errors.New("另一個 CODEX_HOME 正在使用相同 USER_DATA_DIR，請為此帳號設定獨立的資料目錄")
			}
			continue
		}
		if p.Started <= 0 {
			return nil, errors.New("無法確認對應 Codex 程序身分，請手動關閉該實例後再試")
		}
		// 共用 auth.json 的工作階段皆須退出，否則會讀寫被替換的憑證。
		targets = append(targets, p)
	}
	return targets, nil
}
func closeTargetCodex(home, data string, scan func() ([]codexInstance, error), terminate func(codexInstance) error) error {
	list, err := scan()
	if err != nil {
		return err
	}
	targets, err := selectCodexInstances(list, home, data)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil // 沒有對應實例，直接進入後續啟動流程。
	}
	for _, p := range targets {
		if err := terminate(p); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, err = scan()
		if err != nil {
			return err
		}
		targets, err = selectCodexInstances(list, home, data)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("對應 CODEX_HOME 的 Codex App 尚未關閉，請手動關閉後再套用")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
