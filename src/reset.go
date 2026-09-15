package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

const resetEndpoint = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"

type resetCredit struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at"`
}
type resetCredits struct {
	Count   *int          `json:"available_count"`
	Credits []resetCredit `json:"credits"`
}

func (s *usageService) resetAction(a Account, action, creditID, key, endpoint string) (any, error) {
	if action == "list" {
		raw, err := s.accountRequest(a, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		var result resetCredits
		if json.Unmarshal(raw, &result) != nil {
			return nil, errors.New("重置資料格式無效")
		}
		available := []resetCredit{}
		for _, c := range result.Credits {
			expiry, err := time.Parse(time.RFC3339, c.ExpiresAt)
			if c.ID != "" && c.Status == "available" && err == nil && expiry.After(time.Now()) {
				available = append(available, c)
			}
		}
		sort.Slice(available, func(i, j int) bool {
			a, _ := time.Parse(time.RFC3339, available[i].ExpiresAt)
			b, _ := time.Parse(time.RFC3339, available[j].ExpiresAt)
			return a.Before(b)
		})
		result.Credits = available
		return result, nil
	}
	if action != "consume" || strings.TrimSpace(creditID) == "" || strings.TrimSpace(key) == "" {
		return nil, errors.New("無效的重置請求")
	}
	payload, _ := json.Marshal(map[string]string{"redeem_request_id": key, "credit_id": creditID})
	raw, err := s.accountRequest(a, http.MethodPost, endpoint+"/consume", payload)
	if err != nil {
		return nil, err
	}
	var result struct {
		Code         string `json:"code"`
		WindowsReset int    `json:"windows_reset"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil, errors.New("重置結果無法確認，重試會沿用同一請求")
	}
	switch result.Code {
	case "reset", "already_redeemed", "no_credit", "nothing_to_reset":
		s.mu.Lock()
		entry := s.cache[a.ID]
		entry.attempted = time.Time{}
		s.cache[a.ID] = entry
		s.mu.Unlock()
		return result, nil
	default:
		return nil, errors.New("重置結果未知，重試會沿用同一請求")
	}
}
