package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const usageEndpoint = "https://chatgpt.com/backend-api/wham/usage"

type usageWindow struct {
	Seconds   int     `json:"seconds"`
	Remaining float64 `json:"remaining"`
	ResetAt   int64   `json:"reset_at,omitempty"`
}
type accountUsage struct {
	Windows   []usageWindow `json:"windows"`
	UpdatedAt time.Time     `json:"updated_at"`
	Loading   bool          `json:"loading"`
	Error     string        `json:"error,omitempty"`
}
type usageEntry struct {
	value     accountUsage
	attempted time.Time
}
type usageService struct {
	mu       sync.Mutex
	cache    map[string]usageEntry
	slots    chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	client   *http.Client
	endpoint string
}

func newUsageService() *usageService {
	ctx, cancel := context.WithCancel(context.Background())
	return &usageService{cache: make(map[string]usageEntry), slots: make(chan struct{}, 3), ctx: ctx, cancel: cancel,
		client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: usageEndpoint}
}

// 回傳快取並排程背景更新。UI 呼叫不等待網路，且同一帳號不重複請求。
func (s *usageService) snapshot(accounts []Account) map[string]accountUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]accountUsage, len(accounts))
	present := make(map[string]bool, len(accounts))
	for _, a := range accounts {
		present[a.ID] = true
		entry := s.cache[a.ID]
		if !entry.value.Loading && time.Since(entry.attempted) >= time.Minute {
			entry.value.Loading = true
			entry.attempted = time.Now()
			s.cache[a.ID] = entry
			go s.refresh(a)
		}
		result[a.ID] = entry.value
	}
	for id := range s.cache {
		if !present[id] {
			delete(s.cache, id)
		}
	}
	return result
}
func (s *usageService) refresh(a Account) {
	select {
	case s.slots <- struct{}{}:
	case <-s.ctx.Done():
		return
	}
	defer func() { <-s.slots }()
	windows, err := s.fetch(a)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, exists := s.cache[a.ID]
	if !exists {
		return
	}
	entry.value.Loading = false
	if err != nil {
		entry.value.Error = err.Error()
	} else {
		entry.value = accountUsage{Windows: windows, UpdatedAt: time.Now()}
	}
	s.cache[a.ID] = entry
}
func (s *usageService) fetch(a Account) ([]usageWindow, error) {
	body, err := s.accountRequest(a, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return nil, err
	}
	return parseUsage(body)
}
func (s *usageService) accountRequest(a Account, method, endpoint string, payload []byte) ([]byte, error) {
	// 活動登入檔可能已輪替 token；僅在帳號相符時使用，不修改任何憑證。
	if home, _, err := accountDirectories(a); err == nil {
		if raw, err := os.ReadFile(filepath.Join(home, "auth.json")); err == nil {
			if live, err := accountFromAuth(raw); err == nil && strings.EqualFold(live.Email, a.Email) {
				a = live
			}
		}
	}
	var auth codexAuth
	_ = json.Unmarshal(a.Auth, &auth)
	if a.AccessToken == "" {
		return nil, errors.New("請重新登入")
	}
	req, err := http.NewRequestWithContext(s.ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("無法建立用量請求")
	}
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth.Tokens.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", auth.Tokens.AccountID)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("用量暫時無法更新")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errors.New("登入已失效或無權限")
	}
	if resp.StatusCode == 429 {
		return nil, errors.New("請求過於頻繁，稍後重試")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("用量服務 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errors.New("無法讀取用量回應")
	}
	return body, nil
}
func parseUsage(body []byte) ([]usageWindow, error) {
	type window struct {
		Used    *float64 `json:"used_percent"`
		Seconds int      `json:"limit_window_seconds"`
		ResetAt int64    `json:"reset_at"`
	}
	var payload struct {
		RateLimit *struct {
			Primary   *window `json:"primary_window"`
			Secondary *window `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.RateLimit == nil {
		return nil, errors.New("用量資料格式無效")
	}
	result := []usageWindow{}
	for _, w := range []*window{payload.RateLimit.Primary, payload.RateLimit.Secondary} {
		if w == nil {
			continue
		}
		if w.Used == nil || math.IsNaN(*w.Used) || math.IsInf(*w.Used, 0) || w.Seconds <= 0 {
			return nil, errors.New("用量資料不完整")
		}
		result = append(result, usageWindow{Seconds: w.Seconds, Remaining: math.Max(0, math.Min(100, 100-*w.Used)), ResetAt: w.ResetAt})
	}
	if len(result) == 0 {
		return nil, errors.New("此帳號未提供用量資料")
	}
	return result, nil
}
