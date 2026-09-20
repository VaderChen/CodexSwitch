package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func normalizeDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("目錄不可包含換行或控制字元")
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			value = home
		} else {
			value = home + string(filepath.Separator) + strings.TrimPrefix(value, "~/")
		}
	}
	if !filepath.IsAbs(value) {
		return "", errors.New("請輸入絕對路徑或以 ~/ 開頭的目錄")
	}
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
		return "", errors.New("指定路徑不是目錄")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	resolved, err := canonicalProcessPath(value)
	if err != nil {
		return "", err
	}
	for _, part := range strings.Split(value, string(filepath.Separator)) {
		if part == ".." {
			return resolved, nil
		}
	}
	// Preserve the user's symlink spelling unless parent traversal requires
	// resolving it before later filepath.Join calls can clean the path.
	return filepath.Clean(value), nil
}

func (s *accountStore) saveSettings(id, home, data string) error {
	home, data, err := accountDirectories(Account{CodexHome: home, UserDataDir: data})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			old := s.accounts[i]
			s.accounts[i].CodexHome, s.accounts[i].UserDataDir = home, data
			if err := s.saveLocked(); err != nil {
				s.accounts[i] = old
				return err
			}
			return nil
		}
	}
	return errors.New("找不到此帳號")
}

func accountDirectories(a Account) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	codex, data := strings.TrimSpace(a.CodexHome), strings.TrimSpace(a.UserDataDir)
	if codex == "" {
		codex = home + "/.codex"
	}
	if data == "" {
		data = home + "/Library/Application Support/Codex"
	}
	codex, err = normalizeDirectory(codex)
	if err != nil {
		return "", "", err
	}
	data, err = normalizeDirectory(data)
	return codex, data, err
}

type activeProfile struct {
	AuthPath    string `json:"auth_path"`
	UserDataDir string `json:"user_data_dir"`
}

func (m *loginManager) profilePath() string {
	return filepath.Join(filepath.Dir(m.store.path), "active-profile.json")
}
func (m *loginManager) activeAuthPath() (string, error) {
	var p activeProfile
	if b, err := os.ReadFile(m.profilePath()); err == nil && json.Unmarshal(b, &p) == nil && filepath.IsAbs(p.AuthPath) {
		return p.AuthPath, nil
	}
	return authFilePath()
}
