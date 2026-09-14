//go:build darwin

package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"github.com/webview/webview_go"
	"log"
	"strings"
)

//go:embed web/index.html
var indexHTML string

//go:embed web/style.css
var styleCSS string

func main() {
	store, err := newAccountStore()
	if err != nil {
		log.Fatal(err)
	}
	manager := newLoginManager(store)
	w := webview.New(true)
	defer w.Destroy()
	w.SetTitle("CodexSwitch")
	w.SetSize(760, 500, webview.HintNone)
	configureNativeWindow(w.Window())
	w.Bind("setWindowSize", func(width, height int) {
		if width >= 480 && height >= 360 && width <= 2400 && height <= 1600 {
			w.SetSize(width, height, webview.HintNone)
		}
	})
	w.Bind("quitApp", func() string { go func() { w.Terminate() }(); return `{"ok":true}` })
	w.Bind("getAccounts", func() string {
		a := store.list()
		out := make([]PublicAccount, 0, len(a))
		for _, item := range a {
			out = append(out, item.Public())
		}
		b, _ := json.Marshal(out)
		return string(b)
	})
	w.Bind("startLogin", func(email string) string {
		a, err := manager.start(email)
		if err != nil {
			b, _ := json.Marshal(map[string]any{"error": err.Error()})
			return string(b)
		}
		b, _ := json.Marshal(map[string]any{"account": a.Public()})
		return string(b)
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
	w.Bind("useAccount", func(id string) string {
		r, err := manager.useAccount(id)
		if err != nil {
			b, _ := json.Marshal(map[string]any{"error": err.Error()})
			return string(b)
		}
		b, _ := json.Marshal(r)
		return string(b)
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
	return strings.Replace(indexHTML, "/* STYLE_PLACEHOLDER */", styleCSS, 1)
}
