2026-09-20 更新紀錄

本文件記錄第一輪修正；後續追加修正與最新驗證結果見 [後續修正紀錄](FOLLOWUP.md)。

本次完成初次檢查 R1–R10 的修正，並依使用者要求，將開發環境的預設 Go、gofmt 與 CodexSwitch 建置工具鏈統一為 **Go 1.27.1 darwin/arm64**。

| 項目 | 完成內容 |
| --- | --- |
| R1 路徑別名 | 既有路徑以實際檔案身分比對；不存在子目錄依所在磁碟大小寫規則比對。程序選取、共用資料目錄衝突與使用中標示皆套用相同比對方式。 |
| R2 工具鏈 | 安裝官方 Go 1.27.1、驗證下載 SHA-256、更新預設工具鏈與 `src/go.mod`，重新建置 App。建置前先檢查工具鏈，失敗時保留原 dist。 |
| R3 帳號安全存檔 | 使用同目錄、獨占建立的隨機 `.bak` 暫存檔，以 0600 權限同步寫入後原子替換；不覆寫或跟隨既有固定備份。 |
| R4 空白帳號檔 | 空白、無效 JSON 或讀取失敗會回傳明確錯誤並釋放鎖，避免 `(nil, nil)` 與後續崩潰。 |
| R5 多開資料覆寫 | 資料目錄加入跨程序排他鎖，第二個使用相同資料目錄的實例會拒絕啟動；App 結束後釋放鎖。 |
| R6 對話框互相覆蓋 | 移除、套用、設定、重置及關於共用焦點管理；背景 inert、Tab 範圍限制、Escape 關閉與返回焦點，並防止未完成操作被另一對話框覆蓋。 |
| R7 交易目的檔重疊 | 交易開始前拒絕重複路徑、實體別名及 `.bak` 名稱衝突；套用前另檢查 auth、env、profile、accounts 與鎖檔的衝突。 |
| R8 路徑中的波浪號 | 分別處理精確值 `~` 與 `~/` 前綴，保留末端名為 `~` 的合法子目錄。 |
| R9 更新測試 | 修正啟動自動檢查的 fixture，另驗證啟動即發現新版時，第一次按更新會直接安裝。 |
| R10 Git 完整性 | 備份後移除 `.git` 中 266 個已確認 AppleDouble magic 的側錄檔，保留 Git 物件及 refs。 |

系統設定

- 安裝位置：`~/.local/go1.27.1`。
- 共用路徑：`~/.local/go` 指向新安裝，原有 `.zprofile` 與 `.zshrc` 的 GOROOT／PATH 無須改寫。
- 使用者 Go 設定：`GOTOOLCHAIN=go1.27.1`，避免其他專案的自動工具鏈選取改變預設版本；明確的個別程序環境變數仍可覆寫。
- 驗證登入式與互動式 zsh，Go 與 gofmt 均為 1.27.1；所檢查的 Homebrew、其他常見版本管理器及啟動設定未發現另一套啟用設定。
- 舊 SDK 與工具鏈快取保留，但不再作為預設工具鏈。
- 官方套件 `go1.27.1.darwin-arm64.tar.gz` 的 SHA-256：`ee215d57e0ec269c60cc9ceca68e6bda321ba9ee5afe24f4b0988703c2d87d12`，比對 [Go 官方下載清單](https://go.dev/dl/?mode=json) 通過。

驗證結果

| 檢查 | 結果 |
| --- | --- |
| Go 測試與 race detector | 42 個頂層測試通過；1 個子程序 helper 在主測試程序依設計跳過，已由跨程序鎖測試另行呼叫。statement coverage 45.5%，原為 34.6%。 |
| 檔案系統差異 | 實際使用不區分大小寫的系統暫存磁碟及區分大小寫的 HFSX 暫存映像，兩者皆通過。 |
| 前端單元與瀏覽器測試 | 20 項全部通過；Microsoft Edge 無 JavaScript pageerror，含原生 inert 與模擬不支援 inert 的焦點限制。 |
| 靜態與模組檢查 | `go vet ./...`、`go mod verify`、`go mod tidy -diff`、gofmt、三個 command 腳本語法與 `git diff --check` 通過。 |
| 安全掃描 | Go 1.27.1 執行 `govulncheck`：`No vulnerabilities found.` |
| 舊版工具鏈保護 | 強制使用 Go 1.26.1 時建置拒絕執行，既有 dist 內所有一般檔案的 SHA-256 不變。 |
| App 重建 | arm64 App 建置成功；實際二進位建置資訊為 Go 1.27.1，Info.plist 驗證通過。 |
| 本機 DMG | 臨時簽章檢查、磁碟映像驗證與 SHA-256 校驗通過。 |
| Git 完整性 | `git fsck --full` exit 0；僅有原有 dangling tree 提示，未移除這些未引用物件。 |

本機產物為 `dist/CodexSwitch.app` 與 `dist/CodexSwitch-1.26.0920-build-2331-arm64.dmg`。App 的 `1.26.0920` 沿用「1.年份.月日」命名，與 Go 1.27.1 無關。DMG 使用本機測試簽章，未送 Apple 公證或發布至 GitHub。

測試使用模擬帳號與暫存資料，驗證範圍為隔離環境中的函式與介面行為。第三方 webview 編譯仍出現既有 C++ literal operator 棄用警告，不影響本次測試及建置結果。
