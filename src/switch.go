package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type codexAuth struct {
	Mode   string  `json:"auth_mode"`
	APIKey *string `json:"OPENAI_API_KEY"`
	Tokens struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

// Decode identity metadata from a trusted local credential file, not a JWT verification.
func accountFromAuth(data []byte) (Account, error) {
	var auth codexAuth
	if json.Unmarshal(data, &auth) != nil {
		return Account{}, errors.New("Codex 登入檔格式無效")
	}
	if auth.Mode != "" && auth.Mode != "chatgpt" {
		return Account{}, errors.New("目前僅支援 ChatGPT 帳號切換，請使用 ChatGPT 登入後偵測")
	}
	if auth.Tokens.AccessToken == "" || auth.Tokens.RefreshToken == "" {
		return Account{}, errors.New("登入檔缺少完整 token，請重新登入 Codex")
	}
	parts := strings.Split(auth.Tokens.IDToken, ".")
	if len(parts) != 3 {
		return Account{}, errors.New("登入檔缺少帳號識別資訊")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Account{}, errors.New("帳號識別資訊無法解析")
	}
	var claims struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Email == "" {
		return Account{}, errors.New("登入憑證未包含 Email")
	}
	now := time.Now()
	return Account{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(claims.Email)))), Email: claims.Email, AccessToken: auth.Tokens.AccessToken, RefreshToken: auth.Tokens.RefreshToken, TokenType: "Bearer", CreatedAt: now, UpdatedAt: now, Auth: append(json.RawMessage(nil), data...)}, nil
}

// 只讀取目前登入身分，列表更新不會修改已儲存的憑證。
func (m *loginManager) publicAccounts() []PublicAccount {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	var currentEmail string
	if path, err := m.activeAuthPath(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			if current, err := accountFromAuth(data); err == nil {
				currentEmail = current.Email
			}
		}
	}
	accounts := m.store.list()
	out := make([]PublicAccount, 0, len(accounts))
	for _, account := range accounts {
		item := account.Public()
		home, data, _ := accountDirectories(account)
		item.CodexHome, item.UserDataDir = home, data
		activePath, _ := m.activeAuthPath()
		var profile activeProfile
		profileData, _ := os.ReadFile(m.profilePath())
		_ = json.Unmarshal(profileData, &profile)
		_, defaultData, _ := accountDirectories(Account{})
		if profile.UserDataDir == "" {
			profile.UserDataDir = defaultData
		}
		item.IsCurrent = currentEmail != "" && strings.EqualFold(account.Email, currentEmail) && filepath.Join(home, "auth.json") == activePath && data == profile.UserDataDir
		out = append(out, item)
	}
	return out
}

func (m *loginManager) detectCurrent() (Account, error) {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	path, err := m.activeAuthPath()
	if err != nil {
		return Account{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Account{}, fmt.Errorf("無法讀取 Codex 登入檔：%w", err)
	}
	a, err := accountFromAuth(data)
	if err != nil {
		return Account{}, err
	}
	for _, old := range m.store.list() {
		if strings.EqualFold(old.Email, a.Email) {
			a.ID = old.ID
			a.CreatedAt = old.CreatedAt
		}
	}
	// Refresh existing records too: Codex can rotate refresh tokens between switches.
	if err := m.store.upsert(a); err != nil {
		return Account{}, err
	}
	return a, nil
}

type switchResult struct {
	Email   string `json:"email"`
	EnvPath string `json:"env_path"`
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func environmentFile(a Account, authPath string) []byte {
	var key string
	var auth codexAuth
	if json.Unmarshal(a.Auth, &auth) == nil && auth.APIKey != nil {
		key = *auth.APIKey
	}
	text := "# CodexSwitch：在需要的 Shell 使用 source 載入；請勿分享此檔案。\n" +
		"unset OPENAI_API_KEY CODEX_API_KEY CODEX_AUTH_JSON\n" +
		"export CODEX_HOME=" + shellQuote(filepath.Dir(authPath)) + "\n" +
		"export CODEX_AUTH_FILE=" + shellQuote(authPath) + "\n" +
		"export CODEX_EMAIL=" + shellQuote(a.Email) + "\n" +
		"export CODEX_ACCESS_TOKEN=" + shellQuote(a.AccessToken) + "\n" +
		"export CODEX_REFRESH_TOKEN=" + shellQuote(a.RefreshToken) + "\n"
	if a.UserDataDir != "" {
		text += "export USER_DATA_DIR=" + shellQuote(a.UserDataDir) + "\n"
	}
	if key != "" {
		text += "export CODEX_API_KEY=" + shellQuote(key) + "\n"
	}
	return []byte(text)
}

func (m *loginManager) useAccount(id string) (switchResult, error) {
	m.authMu.Lock()
	defer m.authMu.Unlock()
	exe, err := resolveCodexExecutable()
	if err != nil {
		return switchResult{}, err
	}
	var target Account
	for _, a := range m.store.list() {
		if a.ID == id {
			target = a
			break
		}
	}
	if target.ID == "" {
		return switchResult{}, errors.New("找不到此帳號，請重新整理列表")
	}
	home, userData, err := accountDirectories(target)
	if err != nil {
		return switchResult{}, err
	}
	authPath := filepath.Join(home, "auth.json")
	if err != nil {
		return switchResult{}, err
	}
	authPath, err = filepath.Abs(authPath)
	if err != nil {
		return switchResult{}, err
	}
	if validated, err := accountFromAuth(target.Auth); err != nil || !strings.EqualFold(validated.Email, target.Email) {
		return switchResult{}, errors.New("此帳號的登入憑證不完整或不一致，請重新偵測")
	}
	if codexRunning() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = exec.CommandContext(ctx, "osascript", "-e", `tell application id "com.openai.codex" to quit`).Run()
		cancel()
		deadline := time.Now().Add(5 * time.Second)
		for codexRunning() && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
		if codexRunning() {
			return switchResult{}, errors.New("Codex App 無法自動關閉，請先手動完全關閉再套用")
		}
	}
	// Preserve freshly rotated credentials for the outgoing account before replacement.
	outgoingPath, err := m.activeAuthPath()
	if err != nil {
		return switchResult{}, err
	}
	current, err := os.ReadFile(outgoingPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return switchResult{}, err
	}
	if len(current) > 0 {
		active, e := accountFromAuth(current)
		if e == nil {
			for _, old := range m.store.list() {
				if strings.EqualFold(old.Email, active.Email) {
					active.ID = old.ID
					active.CreatedAt = old.CreatedAt
					if e = m.store.upsert(active); e != nil {
						return switchResult{}, e
					}
					if old.ID == id {
						active.CodexHome, active.UserDataDir = target.CodexHome, target.UserDataDir
						target = active
					}
					break
				}
			}
		}
	}
	validated, err := accountFromAuth(target.Auth)
	if err != nil {
		return switchResult{}, errors.New("此帳號缺少完整登入資料，請在 Codex 登入該帳號後重新偵測")
	}
	if !strings.EqualFold(validated.Email, target.Email) {
		return switchResult{}, errors.New("帳號與憑證不一致，請重新偵測")
	}
	// Preserve all Codex metadata (including account_id and last_refresh).
	var doc map[string]json.RawMessage
	if err = json.Unmarshal(target.Auth, &doc); err != nil {
		return switchResult{}, err
	}
	doc["auth_mode"] = json.RawMessage(`"chatgpt"`)
	doc["OPENAI_API_KEY"] = json.RawMessage(`null`)
	next, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return switchResult{}, err
	}
	envPath := os.Getenv("CODEX_SWITCH_ENV_FILE")
	if envPath == "" {
		envPath = filepath.Join(filepath.Dir(m.store.path), "current-account.env")
	}
	envPath, err = filepath.Abs(envPath)
	if err != nil {
		return switchResult{}, err
	}
	if filepath.Clean(envPath) == filepath.Clean(authPath) || filepath.Clean(envPath) == filepath.Clean(m.store.path) {
		return switchResult{}, errors.New("環境設定檔不可與帳號或登入檔使用相同路徑")
	}
	validated.UserDataDir = userData
	profile, _ := json.Marshal(activeProfile{AuthPath: authPath, UserDataDir: userData})
	if err = replaceFiles([]fileUpdate{{authPath, next}, {envPath, environmentFile(validated, authPath)}, {m.profilePath(), profile}}); err != nil {
		return switchResult{}, err
	}
	// Start Codex with the same per-process environment model as runMyCodex.command.
	cmd := exec.Command(exe)
	cmd.Env = append(codexLaunchEnvironment(os.Environ(), filepath.Dir(authPath)), "USER_DATA_DIR="+userData, "CODEX_USER_DATA_DIR="+userData)
	cmd.Args = append(cmd.Args, "--user-data-dir="+userData)
	if err := cmd.Start(); err != nil {
		return switchResult{}, fmt.Errorf("設定已更新，但無法啟動 Codex App：%w", err)
	}
	go func() { _ = cmd.Wait() }()
	return switchResult{Email: validated.Email, EnvPath: envPath}, nil
}

func codexRunning() bool { return nativeCodexRunning() }

type fileUpdate struct {
	path string
	data []byte
}

// Stage private files, back up originals, and roll back on a failed commit.
func replaceFiles(updates []fileUpdate) error {
	type staged struct {
		fileUpdate
		temp   string
		old    []byte
		exists bool
	}
	items := make([]staged, 0, len(updates))
	defer func() {
		for _, i := range items {
			if i.temp != "" {
				_ = os.Remove(i.temp)
			}
		}
	}()
	for _, u := range updates {
		if info, e := os.Lstat(u.path); e == nil && !info.Mode().IsRegular() {
			return errors.New("目標必須為一般檔案")
		}
		old, e := os.ReadFile(u.path)
		exists := e == nil
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(u.path), 0700); e != nil {
			return e
		}
		f, e := os.CreateTemp(filepath.Dir(u.path), ".codexswitch-*")
		if e != nil {
			return e
		}
		items = append(items, staged{u, f.Name(), old, exists})
		if _, e = f.Write(u.data); e != nil {
			f.Close()
			return e
		}
		if e = f.Sync(); e != nil {
			f.Close()
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
	}
	// Refuse to replace an existing backup left by a failed previous attempt.
	backups := []string{}
	for _, i := range items {
		if !i.exists {
			continue
		}
		f, e := os.OpenFile(i.path+".bak", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			for _, p := range backups {
				_ = os.Remove(p)
			}
			return fmt.Errorf("無法建立備份：%w", e)
		}
		backups = append(backups, i.path+".bak")
		_, e = f.Write(i.old)
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			for _, p := range backups {
				_ = os.Remove(p)
			}
			return e
		}
	}
	// Detect a concurrent Codex refresh before committing.
	for _, i := range items {
		actual, e := os.ReadFile(i.path)
		if (i.exists && (e != nil || !bytes.Equal(actual, i.old))) || (!i.exists && !errors.Is(e, os.ErrNotExist)) {
			for _, p := range backups {
				_ = os.Remove(p)
			}
			return errors.New("登入或環境檔剛被其他程式更新，請重試")
		}
	}
	for index, i := range items {
		if e := os.Rename(i.temp, i.path); e != nil {
			for j := index - 1; j >= 0; j-- {
				prev := items[j]
				var restore error
				if prev.exists {
					restore = os.Rename(prev.path+".bak", prev.path)
				} else {
					restore = os.Remove(prev.path)
				}
				if restore != nil {
					return fmt.Errorf("切換失敗且還原失敗，請保留 .bak 備份：%w", restore)
				}
			}
			for _, p := range backups {
				_ = os.Remove(p)
			}
			return e
		}
	}
	for _, p := range backups {
		if e := os.Remove(p); e != nil {
			return fmt.Errorf("切換已寫入，但備份清理失敗：%w", e)
		}
	}
	return nil
}

// OAuth login is loaded from auth.json; do not override it with inherited automation credentials.
func codexLaunchEnvironment(inherited []string, home string) []string {
	blocked := map[string]bool{"USER_DATA_DIR": true, "CODEX_USER_DATA_DIR": true, "CODEX_HOME": true, "CODEX_AUTH_FILE": true, "CODEX_ACCESS_TOKEN": true, "CODEX_REFRESH_TOKEN": true, "CODEX_EMAIL": true, "CODEX_API_KEY": true, "OPENAI_API_KEY": true, "CODEX_AUTH_JSON": true}
	result := []string{}
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if !blocked[key] {
			result = append(result, entry)
		}
	}
	return append(result, "CODEX_HOME="+home)
}

// 以實際 bundle metadata 找執行檔；在修改憑證與關閉 App 之前驗證。
func resolveCodexExecutable() (string, error) {
	check := func(path string) (string, error) {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", fmt.Errorf("Codex 執行檔不存在或無執行權限：%s", path)
		}
		return filepath.Abs(path)
	}
	if exe := os.Getenv("CODEX_EXE"); exe != "" {
		return check(exe)
	}
	candidates := []string{os.Getenv("CODEX_APP_PATH")}
	explicit := candidates[0] != ""
	if !explicit {
		home, _ := os.UserHomeDir()
		candidates = []string{"/Applications/Codex.app", filepath.Join(home, "Applications", "Codex.app"), "/Applications/ChatGPT.app"}
	}
	for _, app := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		id, err := exec.CommandContext(ctx, "/usr/libexec/PlistBuddy", "-c", "Print CFBundleIdentifier", filepath.Join(app, "Contents", "Info.plist")).Output()
		cancel()
		if err != nil || strings.TrimSpace(string(id)) != "com.openai.codex" {
			continue
		}
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		raw, err := exec.CommandContext(ctx, "/usr/libexec/PlistBuddy", "-c", "Print CFBundleExecutable", filepath.Join(app, "Contents", "Info.plist")).Output()
		cancel()
		name := strings.TrimSpace(string(raw))
		if err != nil || name == "" || filepath.Base(name) != name || name == ".." {
			continue
		}
		return check(filepath.Join(app, "Contents", "MacOS", name))
	}
	return "", errors.New("找不到有效的 Codex App，請設定 CODEX_APP_PATH 或 CODEX_EXE")
}
