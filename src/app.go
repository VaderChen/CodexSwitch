package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Account struct {
	CodexHome    string          `json:"codex_home,omitempty"`
	UserDataDir  string          `json:"user_data_dir,omitempty"`
	Auth         json.RawMessage `json:"codex_auth,omitempty"`
	ID           string          `json:"id"`
	Email        string          `json:"email"`
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token,omitempty"`
	TokenType    string          `json:"token_type,omitempty"`
	ExpiresAt    time.Time       `json:"expires_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type PublicAccount struct {
	CodexHome   string    `json:"codex_home"`
	UserDataDir string    `json:"user_data_dir"`
	IsCurrent   bool      `json:"is_current"`
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	TokenType   string    `json:"token_type,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (a Account) Public() PublicAccount {
	return PublicAccount{CodexHome: a.CodexHome, UserDataDir: a.UserDataDir, ID: a.ID, Email: a.Email, TokenType: a.TokenType, ExpiresAt: a.ExpiresAt, UpdatedAt: a.UpdatedAt}
}

type accountStore struct {
	mu       sync.Mutex
	accounts []Account
	path     string
}

func newAccountStore() (*accountStore, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := os.Getenv("CODEX_SWITCH_DATA_DIR")
	if dir == "" {
		dir = filepath.Join(base, "CodexSwitch")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &accountStore{path: filepath.Join(dir, "accounts.json")}
	if data, err := os.ReadFile(s.path); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &s.accounts); err != nil {
			return nil, fmt.Errorf("讀取帳號資料失敗: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *accountStore) list() []Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Account, len(s.accounts))
	copy(result, s.accounts)
	return result
}

func (s *accountStore) upsert(a Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if strings.EqualFold(s.accounts[i].Email, a.Email) {
			old := s.accounts[i]
			a.ID = old.ID
			a.CreatedAt = old.CreatedAt
			a.CodexHome, a.UserDataDir = old.CodexHome, old.UserDataDir
			if a.RefreshToken == "" {
				a.RefreshToken = old.RefreshToken
			}
			if a.TokenType == "" {
				a.TokenType = old.TokenType
			}
			s.accounts[i] = a
			return s.saveLocked()
		}
	}
	s.accounts = append(s.accounts, a)
	return s.saveLocked()
}

func (s *accountStore) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			s.accounts = append(s.accounts[:i], s.accounts[i+1:]...)
			return s.saveLocked()
		}
	}
	return os.ErrNotExist
}

func (s *accountStore) saveLocked() error {
	data, err := json.MarshalIndent(s.accounts, "", "  ")
	if err != nil {
		return err
	}
	// Write through a .bak file first so an interrupted write cannot corrupt the store.
	bak := s.path + ".bak"
	if err := os.WriteFile(bak, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(bak, s.path); err != nil {
		return err
	}
	return nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	Email        string `json:"email"`
}
type callbackResult struct {
	query url.Values
	err   error
}
type pendingLogin struct {
	state, verifier, email string
	result                 chan callbackResult
}

type loginManager struct {
	authMu   sync.Mutex
	mu       sync.Mutex
	pending  *pendingLogin
	server   *http.Server
	listener net.Listener
	store    *accountStore
}

func newLoginManager(store *accountStore) *loginManager { return &loginManager{store: store} }

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (m *loginManager) start(ctx context.Context, email string) (Account, error) {
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	email = strings.TrimSpace(email)
	if !regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`).MatchString(email) {
		return Account{}, errors.New("請輸入有效的 ChatGPT 帳號 Email")
	}
	m.mu.Lock()
	if m.server != nil {
		m.mu.Unlock()
		return Account{}, errors.New("已有登入流程進行中")
	}
	state, err := randomString(24)
	if err != nil {
		m.mu.Unlock()
		return Account{}, err
	}
	verifier, err := randomString(48)
	if err != nil {
		m.mu.Unlock()
		return Account{}, err
	}
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	ln, err := net.Listen("tcp4", "127.0.0.1:1455")
	if err != nil {
		m.mu.Unlock()
		return Account{}, fmt.Errorf("無法啟動登入回呼（1455 埠），請先結束其他登入流程：%w", err)
	}
	p := &pendingLogin{state: state, verifier: verifier, email: email, result: make(chan callbackResult, 1)}
	m.pending, m.listener = p, ln
	m.server = &http.Server{Handler: http.HandlerFunc(m.handleCallback)}
	go m.server.Serve(ln)
	m.mu.Unlock()

	defer m.finish()
	redirect := "http://localhost:1455/auth/callback"
	authURL := os.Getenv("CODEX_AUTH_URL")
	if authURL == "" {
		authURL = "https://auth.openai.com/oauth/authorize"
	}
	clientID := os.Getenv("CODEX_CLIENT_ID")
	if clientID == "" {
		clientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	}
	q := url.Values{"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {"openid profile email offline_access"}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "login_hint": {email}}
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", "pi")
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	if err := openBrowser(authURL + "?" + q.Encode()); err != nil {
		return Account{}, err
	}
	select {
	case r := <-p.result:
		if r.err != nil {
			return Account{}, r.err
		}
		return m.complete(ctx, p, redirect, r.query)
	case <-ctx.Done():
		return Account{}, ctx.Err()
	case <-time.After(5 * time.Minute):
		return Account{}, errors.New("登入逾時，請重新開始")
	}
}

func (m *loginManager) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/auth/callback" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m.mu.Lock()
	p := m.pending
	m.mu.Unlock()
	if p == nil {
		http.Error(w, "No active login", http.StatusGone)
		return
	}
	if r.URL.Query().Get("state") != p.state {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		select {
		case p.result <- callbackResult{err: fmt.Errorf("登入失敗: %s", e)}:
		default:
		}
		fmt.Fprint(w, "登入失敗，請返回 CodexSwitch。")
		return
	}
	select {
	case p.result <- callbackResult{query: r.URL.Query()}:
	default:
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<html><body style="font-family:-apple-system;padding:40px"><h2>CodexSwitch 登入完成</h2><p>可以關閉此分頁並返回應用程式。</p></body></html>`)
}

func (m *loginManager) complete(ctx context.Context, p *pendingLogin, redirect string, q url.Values) (Account, error) {
	t := tokenResponse{}
	if t.AccessToken == "" {
		code := q.Get("code")
		if code == "" {
			return Account{}, errors.New("回呼未包含授權碼或 access token")
		}
		tokenURL := os.Getenv("CODEX_TOKEN_URL")
		if tokenURL == "" {
			tokenURL = "https://auth.openai.com/oauth/token"
		}
		clientID := os.Getenv("CODEX_CLIENT_ID")
		if clientID == "" {
			clientID = "app_EMoamEEZ73f0CkXaXp7hrann"
		}
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {clientID}, "code_verifier": {p.verifier}}
		client := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return Account{}, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			return Account{}, fmt.Errorf("交換 token 失敗: %w", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode/100 != 2 {
			return Account{}, fmt.Errorf("登入授權交換失敗（HTTP %d），請重新登入", resp.StatusCode)
		}
		if err := json.Unmarshal(body, &t); err != nil {
			return Account{}, fmt.Errorf("解析 token 失敗: %w", err)
		}
	}
	if t.AccessToken == "" {
		return Account{}, errors.New("未取得 access token")
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	parts := strings.Split(t.IDToken, ".")
	if len(parts) != 3 {
		return Account{}, errors.New("登入回應缺少 ID token，請重新登入")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Account{}, errors.New("帳號識別資訊無效")
	}
	if json.Unmarshal(payload, &claims) != nil {
		return Account{}, errors.New("帳號識別資訊無效")
	}
	raw, err := json.Marshal(map[string]any{"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "tokens": map[string]string{"id_token": t.IDToken, "access_token": t.AccessToken, "refresh_token": t.RefreshToken, "account_id": claims.Auth.AccountID}, "last_refresh": time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return Account{}, err
	}
	a, err := accountFromAuth(raw)
	if err != nil {
		return Account{}, err
	}
	now := time.Now()
	if t.ExpiresIn > 0 {
		a.ExpiresAt = now.Add(time.Duration(t.ExpiresIn) * time.Second)
	}
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	if err := m.store.upsert(a); err != nil {
		return Account{}, err
	}
	return a, nil
}

func (m *loginManager) finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil {
		_ = m.server.Close()
	}
	m.server, m.listener, m.pending = nil, nil, nil
}

// authFilePath supports explicit overrides without tying storage to the app bundle.
func authFilePath() (string, error) {
	if path := os.Getenv("CODEX_AUTH_FILE"); path != "" {
		return path, nil
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(home, "auth.json"), nil
}

func openBrowser(target string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("此登入流程需要 macOS 的預設瀏覽器")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "open", target).Run(); err != nil {
		return fmt.Errorf("無法開啟預設瀏覽器：%w", err)
	}
	return nil
}

func (s *accountStore) exportFile() error {
	data, err := json.MarshalIndent(s.list(), "", "  ")
	if err != nil {
		return err
	}
	cmd := exec.Command("osascript", "-e", `POSIX path of (choose file name with prompt "匯出 CodexSwitch 帳號" default name "accounts.json")`)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	path := strings.TrimSpace(string(out))
	return os.WriteFile(path, data, 0600)
}

func (s *accountStore) importFile() error {
	out, err := exec.Command("osascript", "-e", `POSIX path of (choose file with prompt "匯入 CodexSwitch 帳號" of type {"public.json"})`).Output()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(strings.TrimSpace(string(out)))
	if err != nil {
		return err
	}
	var accounts []Account
	if err = json.Unmarshal(data, &accounts); err != nil {
		return fmt.Errorf("匯入 JSON 格式錯誤：%w", err)
	}
	for _, a := range accounts {
		if a.Email == "" || len(a.Auth) == 0 {
			continue
		}
		if err = s.upsert(a); err != nil {
			return err
		}
	}
	return nil
}

// 以完整 ID 清單調整順序，拒絕過期或重複的列表。
func (s *accountStore) reorder(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) != len(s.accounts) {
		return errors.New("帳號列表已變更，請重新整理後再排序")
	}
	byID := make(map[string]Account, len(s.accounts))
	for _, a := range s.accounts {
		byID[a.ID] = a
	}
	next := make([]Account, 0, len(ids))
	for _, id := range ids {
		a, ok := byID[id]
		if !ok {
			return errors.New("排序包含重複或不存在的帳號，請重新整理")
		}
		next = append(next, a)
		delete(byID, id)
	}
	previous := s.accounts
	s.accounts = next
	if err := s.saveLocked(); err != nil {
		s.accounts = previous
		return err
	}
	return nil
}
