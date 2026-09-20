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

type controlledUsageReply struct {
	started chan struct{}
	release chan struct{}
}

func usageRaceFixture(t *testing.T) (*usageService, Account, [2]controlledUsageReply) {
	t.Helper()
	replies := [2]controlledUsageReply{}
	for i := range replies {
		replies[i] = controlledUsageReply{started: make(chan struct{}), release: make(chan struct{}, 1)}
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/consume" {
			w.Write([]byte(`{"code":"reset","windows_reset":1}`))
			return
		}
		index := int(calls.Add(1)) - 1
		if index >= len(replies) {
			t.Error("unexpected extra usage request")
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		close(replies[index].started)
		select {
		case <-replies[index].release:
		case <-r.Context().Done():
			return
		}
		if index == 0 {
			w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":90,"limit_window_seconds":18000}}}`))
		} else {
			w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000}}}`))
		}
	}))
	t.Cleanup(server.Close)
	s := newUsageService()
	t.Cleanup(s.cancel)
	s.endpoint = server.URL
	a := Account{ID: "usage-race", Email: "usage@example.invalid", AccessToken: "old", CodexHome: t.TempDir()}
	return s, a, replies
}

func waitUsageRequest(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("usage request did not start")
	}
}

func waitUsageCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("usage refresh did not reach expected state")
}

func assertLatestUsage(t *testing.T, s *usageService, id string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.cache[id].value
	if value.Loading || value.Error != "" || len(value.Windows) != 1 || value.Windows[0].Remaining != 90 {
		t.Fatalf("stale response replaced latest usage: %+v", value)
	}
}

func TestUsageReaddedAccountIgnoresPreviousRequest(t *testing.T) {
	s, a, replies := usageRaceFixture(t)
	s.snapshot([]Account{a})
	waitUsageRequest(t, replies[0].started)
	s.snapshot(nil)
	a.AccessToken = "new"
	s.snapshot([]Account{a})
	waitUsageRequest(t, replies[1].started)
	replies[1].release <- struct{}{}
	waitUsageCondition(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.cache[a.ID].value.Loading
	})
	assertLatestUsage(t, s, a.ID)
	replies[0].release <- struct{}{}
	// refresh 在完成快取回寫後才釋放 slot，可確定晚到的舊請求已結束。
	waitUsageCondition(t, func() bool { return len(s.slots) == 0 })
	assertLatestUsage(t, s, a.ID)
}

func TestUsageResetInvalidatesInflightRequest(t *testing.T) {
	for _, oldFinishesFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "new response first", true: "old response before next snapshot"}[oldFinishesFirst], func(t *testing.T) {
			s, a, replies := usageRaceFixture(t)
			s.snapshot([]Account{a})
			waitUsageRequest(t, replies[0].started)
			if _, err := s.resetAction(a, "consume", "fixture-credit", "fixture-request", s.endpoint); err != nil {
				t.Fatal(err)
			}
			if oldFinishesFirst {
				replies[0].release <- struct{}{}
				waitUsageCondition(t, func() bool { return len(s.slots) == 0 })
				s.mu.Lock()
				entry := s.cache[a.ID]
				s.mu.Unlock()
				if !entry.attempted.IsZero() || entry.value.Loading || len(entry.value.Windows) != 0 {
					t.Fatalf("pre-reset response repopulated invalidated cache: %+v", entry)
				}
			}
			s.snapshot([]Account{a})
			waitUsageRequest(t, replies[1].started)
			replies[1].release <- struct{}{}
			waitUsageCondition(t, func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return !s.cache[a.ID].value.Loading
			})
			assertLatestUsage(t, s, a.ID)
			if !oldFinishesFirst {
				replies[0].release <- struct{}{}
				waitUsageCondition(t, func() bool { return len(s.slots) == 0 })
				assertLatestUsage(t, s, a.ID)
			}
		})
	}
}
