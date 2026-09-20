package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestOAuthCallbackRejectsWrongState(t *testing.T) {
	p := &pendingLogin{state: "fixture-state", result: make(chan callbackResult, 1)}
	m := &loginManager{pending: p}
	w := httptest.NewRecorder()
	m.handleCallback(w, httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong&code=fixture", nil))
	if w.Code != 400 || len(p.result) != 0 {
		t.Fatal("wrong OAuth state accepted")
	}
	w = httptest.NewRecorder()
	m.handleCallback(w, httptest.NewRequest(http.MethodGet, "/auth/callback?state=fixture-state&code=fixture", nil))
	if w.Code != 200 || len(p.result) != 1 {
		t.Fatal("valid OAuth callback rejected")
	}
}

func TestOAuthExchangeAndCancellation(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"email": "oauth@example.invalid", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "fixture-account"}})
	idToken := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Error("wrong token method")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("code_verifier") != "fixture-verifier" || r.Form.Get("grant_type") != "authorization_code" {
			t.Error("missing PKCE verifier")
		}
		json.NewEncoder(w).Encode(tokenResponse{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", IDToken: idToken, ExpiresIn: 3600})
	}))
	defer server.Close()
	t.Setenv("CODEX_TOKEN_URL", server.URL)
	store := &accountStore{path: filepath.Join(t.TempDir(), "accounts.json")}
	m := newLoginManager(store)
	p := &pendingLogin{verifier: "fixture-verifier"}
	a, err := m.complete(context.Background(), p, "http://localhost:1455/auth/callback", url.Values{"code": {"fixture-code"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "oauth@example.invalid" || len(store.list()) != 1 {
		t.Fatal("exchange did not persist actual identity")
	}
	var auth codexAuth
	if err := json.Unmarshal(a.Auth, &auth); err != nil || auth.Tokens.AccountID != "fixture-account" {
		t.Fatal("account id lost")
	}
	before, _ := os.ReadFile(store.path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.complete(ctx, p, "http://localhost:1455/auth/callback", url.Values{"code": {"fixture-code"}}); err == nil {
		t.Fatal("cancelled exchange accepted")
	}
	after, _ := os.ReadFile(store.path)
	if string(before) != string(after) {
		t.Fatal("cancelled exchange modified store")
	}
}

func TestExistingBackupStopsTransactionWithoutWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".bak", []byte("backup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFiles([]fileUpdate{{p, []byte("new")}}); err == nil {
		t.Fatal("existing backup must block commit")
	}
	actual, _ := os.ReadFile(p)
	backup, _ := os.ReadFile(p + ".bak")
	if string(actual) != "old" || string(backup) != "backup" {
		t.Fatal("failed preflight changed data")
	}
}
