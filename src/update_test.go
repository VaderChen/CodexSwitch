package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateVersionOrdering(t *testing.T) {
	for _, tc := range []struct {
		remote string
		want   bool
	}{
		{"v1.26.0915-build-2145", false}, {"1.26.0915-build-2146", true}, {"1.26.0914-build-2359", false}, {"1.26.0916-build-0001", true}, {"CodexSwitch-1.26.0915-build-2145", false},
	} {
		got, err := newerVersion(tc.remote, "1.26.0915 build 2145")
		if err != nil || got != tc.want {
			t.Fatalf("%s %v %v", tc.remote, got, err)
		}
	}
	if _, err := newerVersion("unexpected", "1.26.0915 build 2145"); err == nil {
		t.Fatal("拒絕未知版號")
	}
}
func TestDownloadChecksDigestAndReportsBytes(t *testing.T) {
	content := []byte("signed-dmg-fixture")
	digest := sha256.Sum256(content)
	asset := releaseAsset{Size: int64(len(content)), Digest: "sha256:" + hex.EncodeToString(digest[:])}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("Accept") != "application/octet-stream" {
			t.Error("缺少私人 asset 授權")
		}
		w.Write(content)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "update.dmg")
	var progress int64
	err := downloadRelease(context.Background(), server.URL, "fixture", path, asset, func(n, total int64) {
		progress = n
		if total != asset.Size {
			t.Error("總長度不正確")
		}
	})
	if err != nil || progress != asset.Size {
		t.Fatalf("下載或進度錯誤：%v", err)
	}
	asset.Digest = "sha256:bad"
	bad := filepath.Join(t.TempDir(), "bad.dmg")
	if err = downloadRelease(context.Background(), server.URL, "fixture", bad, asset, func(int64, int64) {}); err == nil {
		t.Fatal("不得安裝雜湊不符的檔案")
	}
	if _, err = os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("必須移除失敗下載")
	}
}
func TestReleaseSelectsArchitectureAndHandlesMissing(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(releaseInfo{Tag: "1.26.0915-build-2146", Assets: []releaseAsset{{ID: 1, Name: "CodexSwitch-1.26.0915-build-2146-arm64.dmg", Size: 10, Digest: digest}, {ID: 2, Name: "CodexSwitch-1.26.0915-build-2146-x86_64.dmg", Size: 10, Digest: digest}}})
	}))
	defer server.Close()
	_, a, found, err := fetchRelease(context.Background(), server.URL, "", "1.26.0915 build 2145", "amd64")
	if err != nil || !found || a.ID != 2 {
		t.Fatalf("架構選擇失敗：%v", err)
	}
	if _, _, _, err = fetchRelease(context.Background(), server.URL+"/missing", "", "1.26.0915 build 2145", "arm64"); err == nil {
		t.Fatal("404 必須解釋無 Release/權限")
	}
}
func TestUpdateRequestCanTimeOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, _, err := fetchRelease(ctx, server.URL, "", "1.26.0915 build 2145", "arm64"); err == nil {
		t.Fatal("請求必須可逾時")
	}
}

func TestForceUpdateSelectsLatestEvenWhenNotNewer(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(releaseInfo{Tag: "1.26.0915-build-2146", Assets: []releaseAsset{{ID: 1, Name: "CodexSwitch-1.26.0915-build-2146-arm64.dmg", Size: 10, Digest: digest}}})
	}))
	defer server.Close()
	for _, current := range []string{"1.26.0915 build 2146", "1.26.0916 build 1200"} {
		if _, _, found, err := fetchReleaseMode(context.Background(), server.URL, "", current, "arm64", false); err != nil || found {
			t.Fatal("一般更新不應重裝相同或舊版")
		}
		if _, a, found, err := fetchReleaseMode(context.Background(), server.URL, "", current, "arm64", true); err != nil || !found || a.ID != 1 {
			t.Fatal("強制更新應選擇最新正式版附件")
		}
	}
	digest = "invalid"
	if _, _, _, err := fetchReleaseMode(context.Background(), server.URL, "", "1.26.0915 build 2146", "arm64", true); err == nil {
		t.Fatal("強制更新不可略過校驗要求")
	}
}
