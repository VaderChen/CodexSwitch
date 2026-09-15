package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResetCreditListAndIdempotency(t *testing.T) {
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(resetCredits{Credits: []resetCredit{{ID: "valid", Status: "available", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}, {ID: "old", Status: "available", ExpiresAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}}})
			return
		}
		var payload map[string]string
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["credit_id"] != "valid" {
			t.Error("錯誤的額度 ID")
		}
		keys = append(keys, payload["redeem_request_id"])
		if len(keys) == 1 {
			w.Write([]byte(`{"code":"reset","windows_reset":1}`))
		} else {
			w.Write([]byte(`{"code":"already_redeemed"}`))
		}
	}))
	defer server.Close()
	s := newUsageService()
	defer s.cancel()
	a := Account{AccessToken: "test", CodexHome: t.TempDir()}
	list, err := s.resetAction(a, "list", "", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.(resetCredits).Credits) != 1 {
		t.Fatal("應排除過期額度")
	}
	for i := 0; i < 2; i++ {
		if _, err = s.resetAction(a, "consume", "valid", "same-key", server.URL); err != nil {
			t.Fatal(err)
		}
	}
	if keys[0] != keys[1] {
		t.Fatal("重試不得更換請求識別")
	}
	if _, err = s.resetAction(a, "consume", "valid", "", server.URL); err == nil {
		t.Fatal("不得送出無識別請求")
	}
}
