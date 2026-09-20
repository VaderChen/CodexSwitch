package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type billingTransport func(*http.Request) (*http.Response, error)

func (f billingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func billingTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func billingTestJWT(email, workspace string) string {
	payload, _ := json.Marshal(map[string]any{"email": email, "sub": "not-a-workspace", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": workspace}})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func billingTestAccount(t *testing.T, email, workspace, token string) Account {
	t.Helper()
	auth := codexAuth{Mode: "chatgpt"}
	auth.Tokens.IDToken = billingTestJWT(email, workspace)
	auth.Tokens.AccessToken, auth.Tokens.RefreshToken, auth.Tokens.AccountID = token, "fixture-refresh", workspace
	raw, err := json.Marshal(auth)
	if err != nil {
		t.Fatal(err)
	}
	a, err := accountFromAuth(raw)
	if err != nil {
		t.Fatal(err)
	}
	a.CodexHome = t.TempDir()
	return a
}

func billingChangeAuth(t *testing.T, a Account, change func(*codexAuth)) Account {
	t.Helper()
	var auth codexAuth
	if err := json.Unmarshal(a.Auth, &auth); err != nil {
		t.Fatal(err)
	}
	change(&auth)
	raw, err := json.Marshal(auth)
	if err != nil {
		t.Fatal(err)
	}
	a.Auth = raw
	return a
}

func TestBillingOfficialRequestAndPublicHistory(t *testing.T) {
	a := billingTestAccount(t, "billing@example.invalid", "workspace-primary", "sensitive-token")
	a.AccessToken = "stale-account-field"
	a = billingChangeAuth(t, a, func(auth *codexAuth) { auth.Tokens.IDToken = billingTestJWT(a.Email, "different-jwt-workspace") })
	s := newBillingService()
	cursor := "opaque+/=&? page"
	invoice := "https://invoice.stripe.com/i/invoice-private-value?s=ap"
	s.client.Transport = billingTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Host != "chatgpt.com" || r.URL.Path != "/backend-api/payments/transaction-history" || r.URL.User != nil {
			t.Fatalf("unexpected billing destination: %s %s", r.Method, r.URL.Redacted())
		}
		q := r.URL.Query()
		if len(q) != 3 || q.Get("account_id") != "workspace-primary" || q.Get("limit") != "4" || q.Get("cursor") != cursor {
			t.Fatalf("unexpected query parameters: %v", q)
		}
		if r.Header.Get("Authorization") != "Bearer sensitive-token" || r.Header.Get("ChatGPT-Account-Id") != "workspace-primary" || r.Header.Get("Accept") != "application/json" || !strings.HasPrefix(r.UserAgent(), "CodexSwitch/") {
			t.Error("billing credentials or identifying headers differ")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 20*time.Second {
			t.Error("billing request must have a bounded 20-second deadline")
		}
		return billingTestResponse(200, `{"transactions":[
		 {"type":"invoice","id":"older","created_at":"2026-08-01T00:00:00Z","amount":2000,"currency":"usd","status":"paid","product":{"type":"subscription","plan":"plus"},"invoice_url":"`+invoice+`"},
		 {"type":"invoice","id":"latest-a","created_at":"2026-09-01T08:00:00+08:00","amount":-500,"currency":"twd","status":"refunded","product":{"type":"credits","plan":"pro"},"invoice_url":"https://evil.example/i/no","download_id":"upstream-injected-id"},
		 {"type":"invoice","id":"latest-b","created_at":"2026-09-01T00:00:00Z","amount":0,"currency":"jpy","status":"void","product":{}}
		],"next_cursor":"next+/="}`), nil
	})
	history, err := s.query(context.Background(), a, cursor)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, transaction := range history.Transactions {
		ids = append(ids, transaction.ID)
	}
	if !reflect.DeepEqual(ids, []string{"latest-a", "latest-b", "older"}) || history.NextCursor != "next+/=" {
		t.Fatalf("history order/cursor incorrect: %v %q", ids, history.NextCursor)
	}
	if history.Transactions[0].Amount != -500 || history.Transactions[0].Product.Plan != "pro" || history.Transactions[0].DownloadID != "" || history.Transactions[2].Amount != 2000 {
		t.Fatal("minor-unit amount, product, or filtered download ID incorrect")
	}
	raw, err := json.Marshal(history)
	if err != nil || strings.Contains(string(raw), "invoice_url") || strings.Contains(string(raw), "invoice-private-value") || strings.Contains(string(raw), "sensitive-token") {
		t.Fatal("public history contains private URL or credentials")
	}
	for _, tx := range history.Transactions {
		if _, err := time.Parse(time.RFC3339, tx.CreatedAt.Format(time.RFC3339)); err != nil {
			t.Fatal("transaction date is not RFC3339")
		}
	}
	ticket := history.Transactions[2].DownloadID
	if len(ticket) != 43 || ticket == a.ID {
		t.Fatal("invoice ticket must be an opaque 256-bit identifier")
	}
	if got, err := s.invoiceURL(a.ID, ticket); err != nil || got != invoice {
		t.Fatal("valid invoice ticket did not resolve")
	}
	if got, err := s.invoiceURL("another-account", ticket); err == nil || got != "" {
		t.Fatal("another account obtained the invoice URL")
	}
	if entries, err := os.ReadDir(a.CodexHome); err != nil || len(entries) != 0 {
		t.Fatal("billing query wrote files into the account directory")
	}
}

func TestBillingEmptyHistoryAndCursorLimit(t *testing.T) {
	a := billingTestAccount(t, "billing@example.invalid", "workspace", "fixture-token")
	s := newBillingService()
	var calls int
	s.client.Transport = billingTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Has("cursor") && len(r.URL.Query().Get("cursor")) != billingMaxCursor {
			t.Error("unexpected cursor")
		}
		return billingTestResponse(200, `{"transactions":[],"next_cursor":""}`), nil
	})
	for _, cursor := range []string{"", strings.Repeat("a", billingMaxCursor)} {
		history, err := s.query(context.Background(), a, cursor)
		if err != nil || history.Transactions == nil || len(history.Transactions) != 0 || history.NextCursor != "" {
			t.Fatalf("empty history should succeed: %+v %v", history, err)
		}
	}
	for _, cursor := range []string{strings.Repeat("a", billingMaxCursor+1), "bad\nnext"} {
		if _, err := s.query(context.Background(), a, cursor); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
	if calls != 2 {
		t.Fatal("invalid cursor sent a network request")
	}
}

func TestBillingCredentialSourcesRemainPaired(t *testing.T) {
	for _, scenario := range []string{"stored", "id-claim fallback", "access-claim fallback", "live rotation", "different live email", "different live workspace", "invalid live auth"} {
		t.Run(scenario, func(t *testing.T) {
			a := billingTestAccount(t, "billing@example.invalid", "workspace", "stored-token")
			wantToken := "stored-token"
			switch scenario {
			case "id-claim fallback":
				a = billingChangeAuth(t, a, func(auth *codexAuth) { auth.Tokens.AccountID = "" })
			case "access-claim fallback":
				wantToken = billingTestJWT("unused@example.invalid", "workspace")
				a = billingChangeAuth(t, a, func(auth *codexAuth) {
					auth.Tokens.AccountID = ""
					auth.Tokens.IDToken = billingTestJWT(a.Email, "")
					auth.Tokens.AccessToken = wantToken
				})
			case "live rotation", "different live email", "different live workspace", "invalid live auth":
				email, workspace := a.Email, "workspace"
				if scenario == "different live email" {
					email = "other@example.invalid"
				}
				if scenario == "different live workspace" {
					workspace = "other-workspace"
				}
				live := billingTestAccount(t, email, workspace, "live-token")
				if scenario == "live rotation" {
					wantToken = "live-token"
				}
				if scenario == "invalid live auth" {
					live.Auth = []byte(`{"invalid":true}`)
				}
				if err := os.WriteFile(filepath.Join(a.CodexHome, "auth.json"), live.Auth, 0600); err != nil {
					t.Fatal(err)
				}
			}
			s := newBillingService()
			s.client.Transport = billingTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer "+wantToken || r.Header.Get("ChatGPT-Account-Id") != "workspace" || r.URL.Query().Get("account_id") != "workspace" {
					t.Error("token and workspace were not selected together")
				}
				return billingTestResponse(200, `{"transactions":[]}`), nil
			})
			if _, err := s.query(context.Background(), a, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBillingRejectsIncompleteCredentialsBeforeRequest(t *testing.T) {
	for _, scenario := range []string{"no owner", "no auth", "bad JSON", "wrong email", "API key", "missing refresh", "missing access", "missing ID token", "missing workspace", "header injection"} {
		t.Run(scenario, func(t *testing.T) {
			a := billingTestAccount(t, "billing@example.invalid", "workspace", "sensitive-token")
			switch scenario {
			case "no owner":
				a.ID = ""
			case "no auth":
				a.Auth = nil
			case "bad JSON":
				a.Auth = []byte("sensitive-token invalid-json")
			case "wrong email":
				a.Email = "other@example.invalid"
			default:
				a = billingChangeAuth(t, a, func(auth *codexAuth) {
					switch scenario {
					case "API key":
						auth.Mode = "apikey"
					case "missing refresh":
						auth.Tokens.RefreshToken = " "
					case "missing access":
						auth.Tokens.AccessToken = ""
					case "missing ID token":
						auth.Tokens.IDToken = ""
					case "missing workspace":
						auth.Tokens.AccountID = ""
						auth.Tokens.IDToken = billingTestJWT(a.Email, "")
					case "header injection":
						auth.Tokens.AccessToken = "sensitive-token\r\nInjected: yes"
					}
				})
			}
			if scenario == "missing workspace" {
				live := billingTestAccount(t, a.Email, "unknown-live-workspace", "live-token")
				if err := os.WriteFile(filepath.Join(a.CodexHome, "auth.json"), live.Auth, 0600); err != nil {
					t.Fatal(err)
				}
			}
			s := newBillingService()
			s.client.Transport = billingTransport(func(*http.Request) (*http.Response, error) {
				t.Error("incomplete credentials were sent to the network")
				return nil, errors.New("forbidden request")
			})
			_, err := s.query(context.Background(), a, "")
			if err == nil || !strings.Contains(err.Error(), "重新登入") || strings.Contains(err.Error(), "sensitive-token") {
				t.Fatalf("credential error must be actionable and private: %v", err)
			}
		})
	}
}

func TestBillingRejectsRedirectAndSanitizesErrors(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308, 401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a := billingTestAccount(t, "billing@example.invalid", "sensitive-workspace", "sensitive-token")
			s := newBillingService()
			var calls, redirects int
			s.client.CheckRedirect = func(*http.Request, []*http.Request) error { redirects++; return nil }
			s.client.Transport = billingTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "chatgpt.com" {
					t.Fatal("billing token escaped the official host")
				}
				response := billingTestResponse(status, "sensitive-token response-body")
				response.Header.Set("Location", "https://evil.example/?sensitive-token")
				return response, nil
			})
			_, err := s.query(context.Background(), a, "private-cursor")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || calls != 1 || redirects != 0 {
				t.Fatalf("unexpected status/redirect handling: %v, calls %d, redirects %d", err, calls, redirects)
			}
			for _, private := range []string{"sensitive-token", "sensitive-workspace", "private-cursor", "evil.example", billingEndpoint} {
				if strings.Contains(err.Error(), private) {
					t.Fatal("billing error disclosed private request/response details")
				}
			}
		})
	}
	a := billingTestAccount(t, "billing@example.invalid", "workspace", "sensitive-token")
	s := newBillingService()
	s.client.Transport = billingTransport(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("sensitive-token connection failure: %s", r.URL.String())
	})
	if _, err := s.query(context.Background(), a, "private-cursor"); err == nil || strings.Contains(err.Error(), "sensitive-token") || strings.Contains(err.Error(), "private-cursor") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("transport failure disclosed private data: %v", err)
	}
}

type billingBrokenBody struct{}

func (billingBrokenBody) Read([]byte) (int, error) {
	return 0, errors.New("sensitive-token read failure")
}
func (billingBrokenBody) Close() error { return nil }

func TestBillingValidatesResponseAndReadLimit(t *testing.T) {
	bodies := map[string]string{
		"empty": "", "missing transactions": `{}`, "null transactions": `{"transactions":null}`, "wrong root": `[]`,
		"invalid JSON": `{"transactions":[`, "trailing JSON": `{"transactions":[]} {}`,
		"invalid date":      `{"transactions":[{"created_at":"invalid","amount":2000}]}`,
		"missing date":      `{"transactions":[{"amount":2000}]}`,
		"missing amount":    `{"transactions":[{"created_at":"2026-09-01T00:00:00Z"}]}`,
		"fractional amount": `{"transactions":[{"created_at":"2026-09-01T00:00:00Z","amount":20.01}]}`,
		"cursor too large":  `{"transactions":[],"next_cursor":"` + strings.Repeat("a", billingMaxCursor+1) + `"}`,
		"oversized body":    `{"transactions":[]}` + strings.Repeat(" ", billingMaxBody),
		"read failure":      "",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			a := billingTestAccount(t, "billing@example.invalid", "workspace", "sensitive-token")
			s := newBillingService()
			s.client.Transport = billingTransport(func(*http.Request) (*http.Response, error) {
				response := billingTestResponse(200, body)
				if name == "read failure" {
					response.Body = billingBrokenBody{}
				}
				return response, nil
			})
			if _, err := s.query(context.Background(), a, ""); err == nil || strings.Contains(err.Error(), "sensitive-token") {
				t.Fatalf("malformed/oversized response accepted or details leaked: %v", err)
			}
		})
	}
	t.Run("exact body limit", func(t *testing.T) {
		a := billingTestAccount(t, "billing@example.invalid", "workspace", "fixture-token")
		s := newBillingService()
		body := `{"transactions":[]}`
		body += strings.Repeat(" ", billingMaxBody-len(body))
		s.client.Transport = billingTransport(func(*http.Request) (*http.Response, error) { return billingTestResponse(200, body), nil })
		if _, err := s.query(context.Background(), a, ""); err != nil {
			t.Fatal(err)
		}
	})
}

func TestBillingQueryHonorsContext(t *testing.T) {
	a := billingTestAccount(t, "billing@example.invalid", "workspace", "fixture-token")
	s := newBillingService()
	s.client.Transport = billingTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.query(ctx, a, ""); err == nil || !strings.Contains(err.Error(), "逾時") {
		t.Fatalf("timeout was not handled: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := s.query(ctx, a, ""); err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("cancellation was not handled: %v", err)
	}
}

func TestBillingInvoiceURLAllowlist(t *testing.T) {
	valid := "https://invoice.stripe.com/i/acct_example/live_example?s=ap"
	if got := safeBillingInvoiceURL(valid); got != valid {
		t.Fatal("valid Stripe invoice URL rejected")
	}
	for _, raw := range []string{
		"", "/i/example", "javascript:alert(1)", "http://invoice.stripe.com/i/example",
		"https://invoice.stripe.com.evil.example/i/example", "https://invoice.stripe.com@evil.example/i/example",
		"https://user:password@invoice.stripe.com/i/example", "https://invoice.stripe.com:8443/i/example",
		"https://invoice.stripe.com:443/i/example", "https://invoice.stripe.com:/i/example",
		"https://invoice.stripe.com/other", "https://invoice.stripe.com/i/", "https://invoice.stripe.com/i///",
		"https://invoice.stripe.com/i/../other", "https://invoice.stripe.com/i/%2e%2e/other",
		"https://invoice.stripe.com/i/%2E/other", "https://invoice.stripe.com/i/\\..\\other",
		"https://invoice.stripe.com/i/example\n", "https://invoice.stripe.com/i/example%0a",
		"https://invoice.stripe.com/i/example?value=%0d", "https://invoice.stripe.com/i/example#%7f",
		"https://invoice.stripe.com/i/example\u0085", "https://invoice.stripe.com/i/%zz",
		"https://invoice.stripe.com/i/" + strings.Repeat("a", billingMaxInvoiceURL),
	} {
		if safeBillingInvoiceURL(raw) != "" {
			t.Errorf("unsafe invoice URL accepted: %q", raw)
		}
	}
}

func TestBillingTicketsExpireAndRemainBounded(t *testing.T) {
	s := newBillingService()
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	ids := make(map[string]bool)
	var first, latest string
	for i := 0; i <= billingMaxTickets; i++ {
		id, err := s.storeInvoice("owner", "https://invoice.stripe.com/i/fixture")
		if err != nil || len(id) != 43 || ids[id] {
			t.Fatal("invoice identifiers must be random and unique")
		}
		ids[id] = true
		if first == "" {
			first = id
		}
		latest = id
		now = now.Add(time.Millisecond)
	}
	if len(s.tickets) != billingMaxTickets {
		t.Fatal("invoice ticket cache exceeded its bound")
	}
	if _, err := s.invoiceURL("owner", first); err == nil {
		t.Fatal("oldest ticket was not evicted")
	}
	for _, owner := range []string{"", "other-owner"} {
		if got, err := s.invoiceURL(owner, latest); err == nil || got != "" {
			t.Fatal("ticket owner was not checked")
		}
	}
	if _, err := s.invoiceURL("owner", latest); err != nil {
		t.Fatal("wrong-owner access consumed a valid ticket")
	}
	now = now.Add(billingTicketLifetime)
	if got, err := s.invoiceURL("owner", latest); err == nil || got != "" {
		t.Fatal("expired invoice ticket was accepted")
	}
	if _, err := s.storeInvoice("owner", "https://invoice.stripe.com/i/next"); err != nil || len(s.tickets) != 1 {
		t.Fatal("expired tickets were not removed when querying new invoices")
	}
}

func TestBillingTicketsSupportConcurrentQueries(t *testing.T) {
	a := billingTestAccount(t, "billing@example.invalid", "workspace", "fixture-token")
	s := newBillingService()
	var calls atomic.Int32
	s.client.Transport = billingTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return billingTestResponse(200, `{"transactions":[{"id":"invoice","created_at":"2026-09-01T00:00:00Z","amount":2000,"invoice_url":"https://invoice.stripe.com/i/fixture"}]}`), nil
	})
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Go(func() {
			history, err := s.query(context.Background(), a, "")
			if err != nil || len(history.Transactions) != 1 {
				t.Errorf("concurrent history query failed: %v", err)
				return
			}
			if _, err := s.invoiceURL(a.ID, history.Transactions[0].DownloadID); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if calls.Load() != 12 {
		t.Fatal("concurrent query lost a request")
	}
}
