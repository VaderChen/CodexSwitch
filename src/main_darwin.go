//go:build darwin

package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/webview/webview_go"
	"log"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed web/index.html
var indexHTML string

//go:embed web/style.css
var styleCSS string

//go:embed assets/CodexSwitch.png
var appIconPNG []byte

// 由 build.command 注入，避免版本隨啟動時間變動。
var appVersion = "1.00.0000"
var appBuild = "0000"

func main() {
	store, err := newAccountStore()
	if err != nil {
		log.Fatal(err)
	}
	manager := newLoginManager(store)
	usage := newUsageService()
	defer usage.cancel()
	w := webview.New(true)
	defer w.Destroy()
	w.SetTitle("CodexSwitch")
	w.SetSize(760, 500, webview.HintNone)
	configureNativeWindow(w.Window())
	w.Bind("saveButtonHints", saveButtonHintsPreference)
	w.Bind("acknowledgeUpdateReady", acknowledgeUpdateReady)
	w.Bind("setWindowSize", func(width, height int) {
		if width >= 480 && height >= 360 && width <= 2400 && height <= 1600 {
			w.SetSize(width, height, webview.HintNone)
		}
	})
	w.Bind("quitApp", func() string { go func() { w.Terminate() }(); return `{"ok":true}` })
	w.Bind("getAccountDefaults", func() map[string]string {
		home, data, _ := accountDirectories(Account{})
		return map[string]string{"codex_home": home, "user_data_dir": data}
	})
	w.Bind("saveAccountSettings", func(id, home, data string) map[string]any {
		if err := store.saveSettings(id, home, data); err != nil {
			return map[string]any{"error": err.Error()}
		}
		return map[string]any{"ok": true}
	})
	w.Bind("reorderAccounts", func(ids []string) map[string]any {
		if err := store.reorder(ids); err != nil {
			return map[string]any{"error": err.Error()}
		}
		return map[string]any{"ok": true}
	})
	// 原生 binding 立即返回，由背景工作回傳結果，避免阻塞介面。
	resetBusy := false
	w.Bind("resetAccountAction", func(action, id, credit, key, requestID string) map[string]any {
		if resetBusy {
			return map[string]any{"error": "重置操作進行中，請稍候"}
		}
		var account Account
		for _, a := range store.list() {
			if a.ID == id {
				account = a
				break
			}
		}
		if account.ID == "" {
			return map[string]any{"error": "找不到此帳號"}
		}
		resetBusy = true
		go func() {
			var result any
			var err error
			if action == "consume" {
				key, err = resetRequestKey(filepath.Dir(store.path), account.ID, credit)
			}
			if err == nil {
				result, err = usage.resetAction(account, action, credit, key, resetEndpoint)
			}
			if err != nil {
				result = map[string]any{"error": err.Error()}
			}
			data, _ := json.Marshal(result)
			rid, _ := json.Marshal(requestID)
			w.Dispatch(func() {
				resetBusy = false
				w.Eval("window.receiveResetResult(" + string(rid) + "," + string(data) + ")")
			})
		}()
		return map[string]any{"pending": true}
	})
	w.Bind("getAccountUsage", func() map[string]accountUsage { return usage.snapshot(store.list()) })
	w.Bind("getAccounts", func() string {
		b, _ := json.Marshal(manager.publicAccounts())
		return string(b)
	})
	updater := &updateService{onRestart: func() { w.Dispatch(func() { w.Terminate() }) }}
	w.Bind("checkForUpdate", func() { updater.check(appVersion + " build " + appBuild) })
	w.Bind("getUpdateStatus", updater.snapshot)
	w.Bind("installUpdate", updater.install)
	w.Bind("openGitHub", func() error { return openBrowser("https://github.com/VaderChen/CodexSwitch") })
	w.Bind("openURL", func(url string) map[string]any {
		if !allowedProjectURL(url) {
			return map[string]any{"error": "不允許開啟此網址"}
		}
		if err := openBrowser(url); err != nil {
			return map[string]any{"error": err.Error()}
		}
		return map[string]any{"ok": true}
	})
	// 登入在背景執行，避免阻塞 WebView 與取消操作。
	var cancelLogin context.CancelFunc
	w.Bind("startLogin", func(email string) string {
		if cancelLogin != nil {
			return `{"error":"已有登入流程進行中"}`
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancelLogin = cancel
		go func() {
			a, err := manager.start(ctx, email)
			result := map[string]any{"account": a.Public()}
			if err != nil {
				result = map[string]any{"error": err.Error()}
			}
			if errors.Is(err, context.Canceled) {
				result = map[string]any{"cancelled": true}
			}
			b, _ := json.Marshal(result)
			w.Dispatch(func() {
				cancel()
				cancelLogin = nil
				w.Eval("window.resolveLogin && window.resolveLogin(" + string(b) + ")")
			})
		}()
		return `{"pending":true}`
	})
	w.Bind("cancelLogin", func() string {
		if cancelLogin != nil {
			cancelLogin()
		}
		return `{"ok":true}`
	})
	w.Bind("detectCurrent", func() string {
		a, err := manager.detectCurrent()
		if err != nil {
			b, _ := json.Marshal(map[string]any{"error": err.Error()})
			return string(b)
		}
		b, _ := json.Marshal(map[string]any{"account": a.Public()})
		return string(b)
	})
	applyBusy := false
	w.Bind("useAccount", func(id string) map[string]any {
		if applyBusy {
			return map[string]any{"error": "正在套用帳號，請稍候"}
		}
		applyBusy = true
		go func() {
			r, err := manager.useAccount(id)
			var result any = r
			if err != nil {
				result = map[string]any{"error": err.Error()}
			}
			b, _ := json.Marshal(result)
			w.Dispatch(func() { applyBusy = false; w.Eval("window.resolveApply && window.resolveApply(" + string(b) + ")") })
		}()
		return map[string]any{"pending": true}
	})
	w.Bind("exportAccounts", func() string {
		if err := store.exportFile(); err != nil {
			b, _ := json.Marshal(map[string]any{"error": err.Error()})
			return string(b)
		}
		return `{"ok":true}`
	})
	w.Bind("importAccounts", func() string {
		if err := store.importFile(); err != nil {
			b, _ := json.Marshal(map[string]any{"error": err.Error()})
			return string(b)
		}
		return `{"ok":true}`
	})
	w.Bind("deleteAccount", func(id string) string {
		if err := store.remove(id); err != nil {
			b, _ := json.Marshal(map[string]any{"error": err.Error()})
			return string(b)
		}
		return `{"ok":true}`
	})
	w.Navigate("data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(indexHTMLWithCSS())))
	w.Run()
}

func indexHTMLWithCSS() string {
	page := strings.Replace(indexHTML, "/* STYLE_PLACEHOLDER */", styleCSS, 1)
	page = strings.Replace(page, "<!-- BUTTON_HINTS -->", strconv.FormatBool(buttonHintsPreference()), 1)
	page = strings.Replace(page, "<!-- APP_ICON -->", base64.StdEncoding.EncodeToString(appIconPNG), 1)
	return strings.Replace(page, "<!-- APP_VERSION -->", appVersion+" build "+appBuild, 1)
}

func allowedProjectURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && (u.Path == "/VaderChen/CodexSwitch" || strings.HasPrefix(u.Path, "/VaderChen/CodexSwitch/"))
}
