package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestUsageParsing(t *testing.T) {
	cases := []struct {
		body      string
		count     int
		remaining float64
		bad       bool
	}{
		{`{"rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":18000},"secondary_window":null}}`, 1, 80, false},
		{`{"rate_limit":{"primary_window":null,"secondary_window":{"used_percent":100,"limit_window_seconds":604800}}}`, 1, 0, false},
		{`{"rate_limit":{"primary_window":{"used_percent":110,"limit_window_seconds":18000}}}`, 1, 0, false},
		{`{"rate_limit":{"primary_window":null,"secondary_window":null}}`, 0, 0, true},
		{`{"rate_limit":{"primary_window":{"limit_window_seconds":18000}}}`, 0, 0, true},
		{`{}`, 0, 0, true},
	}
	for _, c := range cases {
		got, err := parseUsage([]byte(c.body))
		if (err != nil) != c.bad {
			t.Fatalf("解析狀態錯誤: %v", err)
		}
		if !c.bad && (len(got) != c.count || got[0].Remaining != c.remaining) {
			t.Fatal("剩餘用量換算錯誤")
		}
	}
}
func TestUsageBackgroundAndCache(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("ChatGPT-Account-Id") != "test-account" {
			t.Error("缺少授權標頭")
		}
		started <- struct{}{}
		<-release
		w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":30,"limit_window_seconds":18000}}}`))
	}))
	defer server.Close()
	defer close(release)
	s := newUsageService()
	defer s.cancel()
	s.endpoint = server.URL
	accounts := []Account{{ID: "a", Email: "test@example.com", AccessToken: "test-token", CodexHome: t.TempDir(), Auth: []byte(`{"tokens":{"account_id":"test-account"}}`)}}
	result := s.snapshot(accounts)
	if !result["a"].Loading {
		t.Fatal("應背景讀取")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("沒有開始請求")
	}
	done := make(chan struct{})
	go func() { s.snapshot(accounts); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("快取讀取被網路阻塞")
	}
	release <- struct{}{}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		result = s.snapshot(accounts)
		if !result["a"].Loading {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(result["a"].Windows) != 1 || result["a"].Windows[0].Remaining != 70 {
		t.Fatal("背景結果未回寫")
	}
	if calls.Load() != 1 {
		t.Fatal("快取期間重複請求")
	}
}
