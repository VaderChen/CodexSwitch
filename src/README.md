# CodexSwitch

CodexSwitch 是一個 macOS 原生 WebView 小工具：輸入 ChatGPT 帳號後，使用預設瀏覽器完成 OAuth 登入；應用程式在 `127.0.0.1` 的隨機埠建立一次性 callback，收到授權碼後交換 token 並將帳號綁定到本機紀錄。

## 執行

需要 macOS、Go 1.22 以上，以及 Xcode Command Line Tools（WebView 的 Cocoa/WebKit 編譯需求）：

```bash
go run .
go build -o CodexSwitch
```

OAuth 端點與 client id 可透過環境變數指定，方便接入你已註冊的 Codex OAuth client：

```bash
export CODEX_CLIENT_ID="your-client-id"
export CODEX_AUTH_URL="https://auth.openai.com/oauth/authorize"
export CODEX_TOKEN_URL="https://auth.openai.com/oauth/token"
go run .
```

預設的 `codex-switch-desktop` 只是開發佔位值；正式使用時必須換成服務端核發、允許 `http://127.0.0.1:<port>/oauth/callback` 的 client id。

帳號資料位於 `~/Library/Application Support/CodexSwitch/accounts.json`，檔案權限為 `0600`，寫入時會先經過 `.bak` 暫存檔。UI 不會回傳或顯示 access token；正式產品建議改用 macOS Keychain（目前版本為可運作的本機 MVP）。

## OAuth 回呼格式

支援標準 authorization-code 回呼（`code` + `state`），也兼容直接傳入 `access_token`、`refresh_token`、`token_type`、`email` 的本地測試回呼。所有回呼都會驗證一次性 `state`，登入完成後立即關閉本地 HTTP server。

## 偵測目前帳號與路徑參數

按「偵測目前帳號」會讀取 Codex 的 auth.json，從 id_token 取得 Email，僅在列表沒有該 Email 時儲存憑證。介面不會取得金鑰。

可使用環境變數指定絕對路徑：

- CODEX_AUTH_FILE：登入檔完整路徑，優先於 CODEX_HOME。
- CODEX_HOME：Codex 設定目錄，預設 ~/.codex。
- CODEX_SWITCH_DATA_DIR：帳號列表儲存目錄，預設 ~/Library/Application Support/CodexSwitch。

例如：

```bash
CODEX_AUTH_FILE="$HOME/.codex/auth.json" ./run.command
```

## 各帳號目錄設定

點擊帳號左側的齒輪可設定該帳號的 `CODEX_HOME` 與 `USER_DATA_DIR`。
開啟設定時會顯示完整路徑；留空儲存時會自動填入系統預設值：

- `CODEX_HOME`：`~/.codex`
- `USER_DATA_DIR`：`~/Library/Application Support/Codex`

支援絕對路徑與 `~/`，設定於下次「套用」生效。套用時將登入檔寫入指定的 `CODEX_HOME/auth.json`，並以指定的 `--user-data-dir` 啟動 App。重新登入或偵測更新憑證時會保留各帳號的目錄設定。

## macOS 快捷鍵

使用原生選單與目前焦點處理常用系統快捷鍵：

| 快捷鍵 | 功能 |
| --- | --- |
| ⌘C / ⌘V / ⌘X / ⌘A | 複製／貼上／剪下／全選 |
| ⌘Z / ⇧⌘Z | 復原／重做 |
| ⌥⇧⌘V | 貼上並符合樣式 |
| ⌘W / ⌘M | 關閉視窗／縮小 |
| ⌘H / ⌥⌘H | 隱藏程式／隱藏其他程式 |
| ⌃⌘F | 切換全螢幕 |
| ⌘Q | 結束程式 |
| Esc | 關閉設定或取消確認對話框 |

編輯操作依目前焦點及可用狀態啟用；設定正在儲存時不會由 Esc 關閉。
