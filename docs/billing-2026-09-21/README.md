# 帳單查詢與下載

每個帳號新增「帳單」入口。開啟視窗後按「查詢帳單」才讀取交易，依日期倒序顯示產品、金額與付款狀態；支援重新查詢及載入較早紀錄，分頁重複交易會合併，重複游標會停止分頁。

「下載」會在預設瀏覽器開啟 Stripe 帳單頁，讓使用者在頁面下載 PDF 或收據；沒有有效連結的交易停用下載。此流程與參考專案一致，沒有另外抓取或產生 PDF。

管理訂閱、停止續訂與取消方案僅為探索測試，未納入本功能。

## 參考來源

參考 [VaderChen/LoadBalanceProvider](https://github.com/VaderChen/LoadBalanceProvider)，本次檢視的 commit 為 `acd5d7a1ff7f4b676b9fc335db8c1e42cf1d4adf`：

- [codex_billing.go](https://github.com/VaderChen/LoadBalanceProvider/blob/acd5d7a1ff7f4b676b9fc335db8c1e42cf1d4adf/src/proxy/codex_billing.go)：交易紀錄端點、帳戶識別、4 筆分頁、cursor、回應欄位。
- [providers.html](https://github.com/VaderChen/LoadBalanceProvider/blob/acd5d7a1ff7f4b676b9fc335db8c1e42cf1d4adf/website/providers.html)：手動查詢、分頁、貨幣 minor units 顯示，以及 Stripe 帳單頁下載流程。

依現有 CodexSwitch 架構獨立實作後端服務、非同步 WebView bridge 與繁體中文介面；未執行參考專案。

## 實作

- `src/billing.go`：固定 GET `https://chatgpt.com/backend-api/payments/transaction-history`，使用帳號自己的 ChatGPT 工作區 ID。使用即時登入憑證時，同時核對 email 與工作區；不借用其他帳號或其他工作區的憑證。API key、不完整憑證與缺少工作區會提示重新登入，HTTP 401／403／429 提供可重試的錯誤訊息。
- 查詢限時 20 秒、回應限制 2 MiB、cursor 限制 4096 bytes，拒絕 HTTP 重新導向。回傳前驗證日期與整數金額。金額以幣別小數位換算，例如 USD 2000 → USD 20.00、JPY 3000 → JPY 3,000、KWD 12345 → KWD 12.345。
- 只接受 HTTPS 的 `invoice.stripe.com/i/…` 帳單網址，排除帳密、非預期主機／埠、控制字元及路徑跳轉。網址不傳入前端；隨機下載識別碼限定本地帳號、10 分鐘有效、最多保留 256 筆，超量淘汰最舊紀錄。帳單與連結只保留於記憶體，不寫入帳號檔或一般紀錄。
- `src/billing_bridge.go`、`src/main_darwin.go`：非同步查詢及開啟瀏覽器，最多 4 筆同時操作。關閉或取消會終止背景請求；使用請求身分防止取消後晚到的回應取代新查詢。開啟帳單前確認帳號仍存在及下載識別碼所屬帳號。
- `src/web/index.html`、`src/web/style.css`：查詢／下載進行中與錯誤提示、空白狀態、取消與 30 秒介面逾時；關閉或切換帳號後忽略舊回應。交易欄位以文字節點呈現，對話框支援 Escape、Tab 焦點限制與焦點還原。

## 驗證

自動化帳單測試使用模擬帳號、mock HTTP transport 與模擬瀏覽器開啟器，驗證回應解析、分頁及下載流程。線上服務及 Stripe 下載頁的端到端驗收不在這套自動化測試範圍內。

| 檢查 | 結果 |
| --- | --- |
| Go 全套及 race detector | 65 個頂層測試通過、1 個子程序 helper 依設計跳過；statement coverage 54.0%。新增 10 項帳單服務與 4 項 bridge 測試。 |
| 前端全套 | 33 項通過，包含新增的 9 項帳單測試。 |
| 視窗操作 | 760×500、520×500 假資料驗證：12→24 筆分頁可捲動，首末筆下載、查詢、重新查詢與關閉均可操作，帳號按鈕沒有水平溢位。 |
| 靜態及模組 | `go vet ./...`、`go mod verify`、`go mod tidy -diff`、修改 Go 檔的 `gofmt` 與 `git diff --check` 通過。 |
| 建置保全 | 4 項隔離測試通過，包含失敗保留原 App、成功替換與保留既有備份。 |
| 獨立檢查 | 帳號憑證配對、HTTP 限制、下載識別碼、bridge 取消及前端處理均已集中檢查，未發現待修正問題。 |
| 產物 | Go 1.27.1 arm64 建置成功；App 臨時簽章、DMG 映像與 SHA-256 校驗通過。 |

介面示意使用模擬資料，見[專案 README](../../README.md#帳單查詢與下載)。Chromium 測試無法取代 macOS WKWebView 的完整人工驗收。

產物為 `dist/CodexSwitch.app`（1.26.0921 build 0013）及 `dist/CodexSwitch-1.26.0921-build-0013-arm64.dmg`。兩者為本機測試版本，使用臨時簽章，未送 Apple 公證或發布至 GitHub。

直接執行的 App 另在系統暫存目錄完成臨時簽章，複製回專案時不帶額外檔案屬性，避免外接磁碟的 AppleDouble 中繼檔影響驗證；最終 `codesign --verify --deep --strict` 通過。
