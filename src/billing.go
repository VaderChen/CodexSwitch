package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	billingEndpoint       = "https://chatgpt.com/backend-api/payments/transaction-history"
	billingMaxBody        = 2 << 20
	billingMaxCursor      = 4096
	billingMaxInvoiceURL  = 16 << 10
	billingMaxTickets     = 256
	billingTicketLifetime = 10 * time.Minute
)

type billingProduct struct {
	Type string `json:"type"`
	Plan string `json:"plan"`
}

type billingTransaction struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	CreatedAt  time.Time      `json:"created_at"`
	Amount     int64          `json:"amount"`
	Currency   string         `json:"currency"`
	Status     string         `json:"status"`
	Product    billingProduct `json:"product"`
	DownloadID string         `json:"download_id,omitempty"`
}

type billingHistory struct {
	Transactions []billingTransaction `json:"transactions"`
	NextCursor   string               `json:"next_cursor"`
}

type billingTicket struct {
	owner, url string
	expiresAt  time.Time
}

type billingService struct {
	client  *http.Client
	mu      sync.Mutex
	tickets map[string]billingTicket
	now     func() time.Time
}

func newBillingService() *billingService {
	return &billingService{
		client:  &http.Client{Timeout: 20 * time.Second},
		tickets: make(map[string]billingTicket),
		now:     time.Now,
	}
}

type billingCredential struct {
	accessToken, accountID string
}

func parseBillingCredential(raw json.RawMessage, email string) (billingCredential, error) {
	invalid := errors.New("帳單查詢需要完整的 ChatGPT 登入憑證與帳戶識別，請重新登入")
	identity, err := accountFromAuth(raw)
	if err != nil || email == "" || !strings.EqualFold(identity.Email, email) {
		return billingCredential{}, invalid
	}
	var auth codexAuth
	if json.Unmarshal(raw, &auth) != nil {
		return billingCredential{}, invalid
	}
	accountID := strings.TrimSpace(auth.Tokens.AccountID)
	if accountID == "" {
		for _, token := range []string{auth.Tokens.IDToken, auth.Tokens.AccessToken} {
			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				continue
			}
			payload, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				continue
			}
			var claims struct {
				Auth struct {
					AccountID string `json:"chatgpt_account_id"`
				} `json:"https://api.openai.com/auth"`
			}
			if json.Unmarshal(payload, &claims) == nil {
				accountID = strings.TrimSpace(claims.Auth.AccountID)
				if accountID != "" {
					break
				}
			}
		}
	}
	if accountID == "" || strings.TrimSpace(auth.Tokens.AccessToken) == "" || strings.TrimSpace(auth.Tokens.RefreshToken) == "" || billingHasControl(accountID) || billingHasControl(auth.Tokens.AccessToken) {
		return billingCredential{}, invalid
	}
	return billingCredential{accessToken: auth.Tokens.AccessToken, accountID: accountID}, nil
}

func billingCredentials(a Account) (billingCredential, error) {
	stored, err := parseBillingCredential(a.Auth, a.Email)
	if err != nil {
		return billingCredential{}, err
	}
	// 同一 email 可能屬於不同 workspace；輪替 token 必須與儲存的 workspace 成對。
	if home, _, err := accountDirectories(a); err == nil {
		if raw, err := os.ReadFile(filepath.Join(home, "auth.json")); err == nil {
			if live, err := parseBillingCredential(raw, a.Email); err == nil && live.accountID == stored.accountID {
				return live, nil
			}
		}
	}
	return stored, nil
}

func (s *billingService) query(ctx context.Context, a Account, cursor string) (billingHistory, error) {
	if len(cursor) > billingMaxCursor || billingHasControl(cursor) {
		return billingHistory{}, errors.New("帳單分頁資訊無效，請重新查詢")
	}
	if a.ID == "" {
		return billingHistory{}, errors.New("找不到帳單所屬帳號，請重新登入")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	auth, err := billingCredentials(a)
	if err != nil {
		return billingHistory{}, err
	}
	params := url.Values{"account_id": {auth.accountID}, "limit": {"4"}}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, billingEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return billingHistory{}, errors.New("無法建立帳單查詢")
	}
	req.Header.Set("Authorization", "Bearer "+auth.accessToken)
	req.Header.Set("ChatGPT-Account-Id", auth.accountID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CodexSwitch/"+appVersion)
	// 固定官方站台；即使測試或共用 client 設定改變，也不允許帶憑證跟隨轉址。
	client := *s.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return billingHistory{}, errors.New("帳單查詢已取消")
		}
		return billingHistory{}, errors.New("帳單查詢連線失敗或逾時，請稍後重試")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return billingHistory{}, errors.New("帳單登入已失效（HTTP 401），請重新登入")
	case http.StatusForbidden:
		return billingHistory{}, errors.New("此帳號無權讀取帳單（HTTP 403），請確認帳號或重新登入")
	case http.StatusTooManyRequests:
		return billingHistory{}, errors.New("帳單查詢過於頻繁（HTTP 429），請稍後重試")
	default:
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return billingHistory{}, fmt.Errorf("帳單查詢拒絕重新導向（HTTP %d）", resp.StatusCode)
		}
		return billingHistory{}, fmt.Errorf("帳單服務暫時無法使用（HTTP %d）", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, billingMaxBody+1))
	if err != nil || len(raw) > billingMaxBody {
		return billingHistory{}, errors.New("帳單回應不完整或超過大小限制")
	}
	var response struct {
		Transactions []struct {
			billingTransaction
			Amount     *int64     `json:"amount"`
			CreatedAt  *time.Time `json:"created_at"`
			InvoiceURL string     `json:"invoice_url"`
		} `json:"transactions"`
		NextCursor string `json:"next_cursor"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Transactions == nil || len(response.NextCursor) > billingMaxCursor || billingHasControl(response.NextCursor) {
		return billingHistory{}, errors.New("帳單回應格式不正確")
	}
	for _, item := range response.Transactions {
		if item.Amount == nil || item.CreatedAt == nil || item.CreatedAt.IsZero() {
			return billingHistory{}, errors.New("帳單回應缺少有效日期或金額")
		}
	}
	sort.SliceStable(response.Transactions, func(i, j int) bool {
		return response.Transactions[i].CreatedAt.After(*response.Transactions[j].CreatedAt)
	})
	history := billingHistory{Transactions: make([]billingTransaction, 0, len(response.Transactions)), NextCursor: response.NextCursor}
	for _, item := range response.Transactions {
		transaction := item.billingTransaction
		transaction.Amount, transaction.CreatedAt, transaction.DownloadID = *item.Amount, *item.CreatedAt, ""
		if invoice := safeBillingInvoiceURL(item.InvoiceURL); invoice != "" {
			transaction.DownloadID, err = s.storeInvoice(a.ID, invoice)
			if err != nil {
				return billingHistory{}, err
			}
		}
		history.Transactions = append(history.Transactions, transaction)
	}
	return history, nil
}

func billingHasControl(value string) bool {
	return !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0
}

func safeBillingInvoiceURL(raw string) string {
	if raw == "" || len(raw) > billingMaxInvoiceURL || billingHasControl(raw) {
		return ""
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || billingHasControl(decoded) || strings.Contains(decoded, "\\") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "invoice.stripe.com" || u.User != nil || !strings.HasPrefix(u.Path, "/i/") || strings.Trim(u.Path[3:], "/") == "" {
		return ""
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return ""
		}
	}
	return u.String()
}

func (s *billingService) storeInvoice(owner, invoice string) (string, error) {
	id, err := randomString(32)
	if err != nil {
		return "", errors.New("無法準備發票下載，請重新查詢")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, ticket := range s.tickets {
		if !now.Before(ticket.expiresAt) {
			delete(s.tickets, key)
		}
	}
	if len(s.tickets) >= billingMaxTickets {
		var oldest string
		for key, ticket := range s.tickets {
			if oldest == "" || ticket.expiresAt.Before(s.tickets[oldest].expiresAt) {
				oldest = key
			}
		}
		delete(s.tickets, oldest)
	}
	s.tickets[id] = billingTicket{owner: owner, url: invoice, expiresAt: now.Add(billingTicketLifetime)}
	return id, nil
}

// accountID 是本地 Account.ID；URL 不經由 WebView 傳遞或寫入磁碟。
func (s *billingService) invoiceURL(accountID, downloadID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, exists := s.tickets[downloadID]
	if exists && !s.now().Before(ticket.expiresAt) {
		delete(s.tickets, downloadID)
		exists = false
	}
	if !exists || accountID == "" || ticket.owner != accountID {
		return "", errors.New("發票下載已失效，請重新查詢此帳號的帳單")
	}
	return ticket.url, nil
}
