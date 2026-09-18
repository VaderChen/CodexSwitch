package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const releaseAPI = "https://api.github.com/repos/VaderChen/CodexSwitch/releases"

var versionPattern = regexp.MustCompile(`^(?:CodexSwitch-)?v?(\d+)\.(\d+)\.(\d+)(?:-build-| build )(\d+)$`)

func versionParts(value string) ([4]int, error) {
	var result [4]int
	match := versionPattern.FindStringSubmatch(value)
	if match == nil {
		return result, errors.New("Release 版號格式不支援，應為 1.YY.MMDD-build-HHmm")
	}
	for i := range result {
		n, e := strconv.Atoi(match[i+1])
		if e != nil {
			return result, e
		}
		result[i] = n
	}
	return result, nil
}
func newerVersion(remote, current string) (bool, error) {
	a, e := versionParts(remote)
	if e != nil {
		return false, e
	}
	b, e := versionParts(current)
	if e != nil {
		return false, e
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i], nil
		}
	}
	return false, nil
}

type releaseAsset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}
type releaseInfo struct {
	Tag        string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}
type updateStatus struct {
	Phase      string `json:"phase"`
	Message    string `json:"message"`
	Version    string `json:"version"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
}
type updateService struct {
	mu             sync.Mutex
	state          updateStatus
	asset          releaseAsset
	version, token string
	onRestart      func()
}

func (u *updateService) snapshot() updateStatus { u.mu.Lock(); defer u.mu.Unlock(); return u.state }
func (u *updateService) fail(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Phase = "error"
	u.state.Message = err.Error()
}
func githubToken() string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	command, err := exec.LookPath("gh")
	if err != nil {
		for _, path := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh"} {
			if _, e := os.Stat(path); e == nil {
				command = path
				break
			}
		}
	}
	if command == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, command, "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func githubRequest(ctx context.Context, url, token, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "CodexSwitch")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("下載重新導向過多")
		}
		if req.URL.Scheme != "https" {
			return errors.New("拒絕不安全的下載重新導向")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}}
	return client.Do(req)
}
func fetchRelease(ctx context.Context, endpoint, token, current, arch string) (releaseInfo, releaseAsset, bool, error) {
	return fetchReleaseMode(ctx, endpoint, token, current, arch, false)
}
func fetchReleaseMode(ctx context.Context, endpoint, token, current, arch string, force bool) (releaseInfo, releaseAsset, bool, error) {
	var r releaseInfo
	resp, err := githubRequest(ctx, endpoint, token, "application/vnd.github+json")
	if err != nil {
		return r, releaseAsset{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return r, releaseAsset{}, false, errors.New("尚無已發布的 Release，或沒有此私人倉庫的讀取權限；請先使用 gh auth login 登入有權限的 GitHub 帳號")
	}
	if resp.StatusCode != 200 {
		return r, releaseAsset{}, false, fmt.Errorf("GitHub 查詢失敗（HTTP %d）", resp.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&r); err != nil {
		return r, releaseAsset{}, false, err
	}
	if r.Draft || r.Prerelease {
		return r, releaseAsset{}, false, errors.New("此版本不是正式發布版")
	}
	newer, err := newerVersion(r.Tag, current)
	if err != nil || (!newer && !force) {
		return r, releaseAsset{}, false, err
	}
	if arch == "amd64" {
		arch = "x86_64"
	}
	normalized := strings.TrimPrefix(strings.TrimPrefix(r.Tag, "CodexSwitch-"), "v")
	normalized = strings.Replace(normalized, " build ", "-build-", 1)
	for _, a := range r.Assets {
		if a.Name == "CodexSwitch-"+normalized+"-"+arch+".dmg" && a.ID > 0 && a.Size > 0 && a.Size <= 1<<30 {
			if !strings.HasPrefix(a.Digest, "sha256:") || len(a.Digest) != 71 {
				return r, a, false, errors.New("Release 缺少 SHA-256 校驗資訊，無法安全自動安裝")
			}
			if _, err := hex.DecodeString(strings.TrimPrefix(a.Digest, "sha256:")); err != nil {
				return r, a, false, errors.New("Release SHA-256 格式錯誤")
			}
			return r, a, true, nil
		}
	}
	return r, releaseAsset{}, false, errors.New("Release 未提供符合本機架構的 CodexSwitch DMG")
}
func (u *updateService) check(current string) { u.checkMode(current, false) }
func (u *updateService) checkMode(current string, force bool) {
	u.mu.Lock()
	if u.state.Phase == "checking" || u.state.Phase == "downloading" || u.state.Phase == "installing" || u.state.Phase == "restarting" {
		u.mu.Unlock()
		return
	}
	u.state = updateStatus{Phase: "checking", Message: "正在檢查 GitHub Releases…"}
	u.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		token := githubToken()
		r, a, newer, err := fetchReleaseMode(ctx, releaseAPI+"/latest", token, current, runtime.GOARCH, force)
		if err != nil {
			u.fail(err)
			return
		}
		u.mu.Lock()
		u.asset, u.version, u.token = a, r.Tag, token
		u.state.Version = r.Tag
		if newer {
			u.state.Phase = "available"
			u.state.Message = "發現新版本，可下載並安裝。"
		} else {
			u.state.Phase = "current"
			u.state.Message = "目前已是最新版本。"
		}
		u.mu.Unlock()
		if force && newer {
			u.install()
		}
	}()
}
func downloadRelease(ctx context.Context, endpoint, token, path string, a releaseAsset, progress func(int64, int64)) error {
	resp, err := githubRequest(ctx, endpoint, token, "application/octet-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("下載失敗（HTTP %d）", resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		f.Close()
		if !success {
			os.Remove(path)
		}
	}()
	hash := sha256.New()
	buf := make([]byte, 64*1024)
	var count int64
	for {
		n, e := resp.Body.Read(buf)
		if n > 0 {
			count += int64(n)
			if count > a.Size {
				return errors.New("下載大小超出 Release 宣告")
			}
			if _, err = f.Write(buf[:n]); err != nil {
				return err
			}
			hash.Write(buf[:n])
			progress(count, a.Size)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	if count != a.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != a.Digest {
		return errors.New("下載不完整或 SHA-256 不符，已取消安裝")
	}
	if err = f.Sync(); err != nil {
		return err
	}
	success = true
	return f.Close()
}
func (u *updateService) install() {
	u.mu.Lock()
	if u.state.Phase != "available" {
		u.mu.Unlock()
		return
	}
	a, version, token := u.asset, u.version, u.token
	u.state.Phase = "downloading"
	u.state.Total = a.Size
	u.state.Downloaded = 0
	u.state.Message = "正在下載更新…"
	u.mu.Unlock()
	go func() {
		target, err := installedBundle()
		if err != nil {
			u.fail(err)
			return
		}
		stage, err := os.MkdirTemp("", "codexswitch-update-*")
		if err != nil {
			u.fail(err)
			return
		}
		defer os.RemoveAll(stage)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err = downloadRelease(ctx, fmt.Sprintf("%s/assets/%d", releaseAPI, a.ID), token, stage+"/update.dmg", a, func(n, total int64) { u.mu.Lock(); u.state.Downloaded = n; u.state.Total = total; u.mu.Unlock() })
		if err != nil {
			u.fail(err)
			return
		}
		u.mu.Lock()
		u.state.Phase = "installing"
		u.state.Message = "正在驗證簽章並準備更新…"
		u.mu.Unlock()
		if err = stageAndLaunchUpdate(ctx, stage+"/update.dmg", target, version); err != nil {
			u.fail(err)
			return
		}
		u.mu.Lock()
		u.state.Phase = "restarting"
		u.state.Message = "更新已準備完成，正在重新啟動…"
		u.mu.Unlock()
		if u.onRestart != nil {
			u.onRestart()
		}
	}()
}
