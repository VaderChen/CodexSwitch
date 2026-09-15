//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func plistValue(ctx context.Context, bundle, key string) (string, error) {
	b, err := exec.CommandContext(ctx, "/usr/libexec/PlistBuddy", "-c", "Print "+key, filepath.Join(bundle, "Contents", "Info.plist")).Output()
	return strings.TrimSpace(string(b)), err
}
func bundleTeam(ctx context.Context, bundle string) (string, error) {
	if err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", bundle).Run(); err != nil {
		return "", errors.New("App 簽章驗證失敗，已停止自動更新")
	}
	data, err := exec.CommandContext(ctx, "/usr/bin/codesign", "-dv", "--verbose=4", bundle).CombinedOutput()
	if err != nil {
		return "", errors.New("無法讀取 App 簽署資訊")
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "TeamIdentifier=") {
			team := strings.TrimPrefix(line, "TeamIdentifier=")
			if team != "" && team != "not set" {
				return team, nil
			}
		}
	}
	return "", errors.New("目前 App 為本機未簽署版本；請先安裝 Developer ID 簽署版本，才能安全自動更新")
}
func installedBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	target := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(target) != ".app" {
		return "", errors.New("請從已安裝的 CodexSwitch.app 執行更新")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	id, err := plistValue(ctx, target, "CFBundleIdentifier")
	if err != nil || id != "com.vader.codexswitch" {
		return "", errors.New("目前 App 識別不正確")
	}
	if _, err = bundleTeam(ctx, target); err != nil {
		return "", err
	}
	if _, err = os.Lstat(target + ".update.bak"); !os.IsNotExist(err) {
		return "", errors.New("已有更新備份，請先確認前次更新結果")
	}
	probe, err := os.MkdirTemp(filepath.Dir(target), ".codexswitch-write-check-*")
	if err != nil {
		return "", errors.New("App 所在目錄不可寫入；請移至可寫入的應用程式目錄後重試")
	}
	os.Remove(probe)
	return target, nil
}

// Helper waits for this process to exit, retains the original App until reopening succeeds.
const updateHelper = `#!/bin/sh
set -eu
parent="$1"
target="$2"
work="$3"
backup="$target.update.bak"
i=0
while kill -0 "$parent" 2>/dev/null; do
 i=$((i+1))
 if [ "$i" -gt 30 ]; then echo '原程式尚未結束，取消更新。'; exit 1; fi
 sleep 1
done
if [ -e "$backup" ]; then echo '備份已存在，取消更新。'; exit 1; fi
/bin/mv "$target" "$backup"
if ! /bin/mv "$work/CodexSwitch.app" "$target"; then
 /bin/mv "$backup" "$target"
 /usr/bin/open -n "$target"
 exit 1
fi
if ! /usr/bin/open -n "$target"; then
 /bin/mv "$target" "$work/failed.app"
 /bin/mv "$backup" "$target"
 /usr/bin/open -n "$target"
 exit 1
fi
/bin/rm -rf "$backup"
/bin/rm -rf "$work"
`

func stageAndLaunchUpdate(ctx context.Context, dmg, target, version string) error {
	team, err := bundleTeam(ctx, target)
	if err != nil {
		return err
	}
	mount, err := os.MkdirTemp("", "codexswitch-mount-*")
	if err != nil {
		return err
	}
	defer os.Remove(mount)
	if err = exec.CommandContext(ctx, "/usr/bin/hdiutil", "attach", "-readonly", "-nobrowse", "-mountpoint", mount, dmg).Run(); err != nil {
		return errors.New("無法掛載更新 DMG")
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = exec.CommandContext(c, "/usr/bin/hdiutil", "detach", mount).Run()
	}()
	candidate := filepath.Join(mount, "CodexSwitch.app")
	info, err := os.Lstat(candidate)
	if err != nil || !info.IsDir() {
		return errors.New("DMG 未包含 CodexSwitch.app")
	}
	id, err := plistValue(ctx, candidate, "CFBundleIdentifier")
	if err != nil || id != "com.vader.codexswitch" {
		return errors.New("更新 App 識別不符")
	}
	display, err := plistValue(ctx, candidate, "CodexSwitchDisplayVersion")
	if err != nil {
		return errors.New("更新 App 缺少版本資訊")
	}
	actual, err := versionParts(display)
	if err != nil {
		return err
	}
	expected, err := versionParts(version)
	if err != nil || actual != expected {
		return errors.New("更新 App 版本與 Release 不符")
	}
	nextTeam, err := bundleTeam(ctx, candidate)
	if err != nil {
		return err
	}
	if nextTeam != team {
		return errors.New("更新 App 與目前 App 的簽署團隊不同")
	}
	if err = exec.CommandContext(ctx, "/usr/sbin/spctl", "--assess", "--type", "execute", candidate).Run(); err != nil {
		return errors.New("更新 App 未通過 macOS 安全驗證")
	}
	work, err := os.MkdirTemp(filepath.Dir(target), ".codexswitch-update-*")
	if err != nil {
		return err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			os.RemoveAll(work)
		}
	}()
	staged := filepath.Join(work, "CodexSwitch.app")
	if err = exec.CommandContext(ctx, "/usr/bin/ditto", "--norsrc", candidate, staged).Run(); err != nil {
		return errors.New("無法複製更新 App")
	}
	// 外接磁碟的 AppleDouble 不可混入已簽署資源。
	if err = filepath.WalkDir(staged, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if strings.HasPrefix(d.Name(), "._") && !d.IsDir() {
			return os.Remove(path)
		}
		return nil
	}); err != nil {
		return err
	}
	if verified, e := bundleTeam(ctx, staged); e != nil || verified != team {
		return errors.New("複製後的更新簽章驗證失敗")
	}
	script := filepath.Join(work, "finish.sh")
	if err = os.WriteFile(script, []byte(updateHelper), 0700); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(work, "installer.log"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command("/bin/sh", script, fmt.Sprint(os.Getpid()), target, work)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return err
	}
	handedOff = true
	go func() { _ = cmd.Wait() }()
	return nil
}
