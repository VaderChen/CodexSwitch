CodexSwitch 深度檢查紀錄，2026-09-20。

後續修正已完成；Go 工具鏈依需求統一為 1.27.1。修正內容、重建產物與驗證結果見 [更新紀錄](UPDATE.md)。下文保留初次檢查時的狀態與證據。

檢查版本：`2eabe4d0f23549c1860812a15e92b692a618c503`。初次檢查確認 10 項問題：7 項產品程式缺陷、1 項建置工具鏈安全問題，以及 2 項測試／倉庫維護問題。初次檢查時未修改產品程式碼；下文保留當時結果。

| 編號 | 優先級 | 問題 | 驗證方式 |
| --- | --- | --- | --- |
| R1 | P1 | 路徑大小寫不同會漏掉共用登入檔的執行中實例 | 在不分大小寫的本機磁碟重現 |
| R2 | P1 | 現有工具鏈與產物使用 Go 1.26.1，掃描標記 9 份標準庫安全公告 | govulncheck、產物建置資訊、官方公告 |
| R3 | P2 | 帳號存檔沿用既有 `.bak` 權限，並跟隨符號連結 | 兩個獨立測試重現 |
| R4 | P2 | 空白帳號檔讓初始化回傳 `(nil, nil)` | 單元測試重現 |
| R5 | P2 | 多開 CodexSwitch 會以舊快照覆寫另一實例的帳號資料 | 兩個獨立 store 重現 |
| R6 | P2 | 鍵盤可操作對話框後方按鈕，覆蓋對話框後永久卡住移除流程 | Playwright 實際 Tab／Enter 操作 |
| R7 | P2 | 檔案交易未拒絕重複目的路徑，可回報成功但覆蓋前一份內容 | 單元測試重現 |
| R8 | P3 | `~/某目錄/~` 被誤轉成使用者家目錄 | 單元測試重現 |
| R9 | P2 | 更新瀏覽器測試未配合啟動自動檢查，整套前端測試失敗 | 原測試失敗；只修暫存副本 fixture 後通過 |
| R10 | P2 | Git 內的 AppleDouble 檔案使 `git fsck` 失敗 | 原倉庫失敗；排除側錄檔的副本通過 |

P1 表示建議優先處理，P2 為應修正的功能／可靠性問題，P3 為較少見的邊界問題。安全掃描結果並不等同於已成功利用漏洞；條件與限制列於各項說明。

檢查涵蓋全部 42 個已追蹤檔案、約 4,421 行程式／測試／文件與本機封裝腳本；包括 Go 帳號儲存、OAuth callback 與 token 交換、程序辨識、切換、用量與重置、更新下載與 helper、Objective-C 橋接、HTML／CSS／JavaScript，以及建置與 DMG 封裝。測試在獨立 `.bak` 副本進行，使用假帳號和本機模擬 HTTP 服務。

| 檢查 | 結果 |
| --- | --- |
| 既有 Go 測試，`go test -race -coverprofile=... -timeout=120s ./...` | 25 個頂層測試全部通過；已覆蓋路徑未報告 Go 資料競態；statement coverage 34.6% |
| `go vet ./...`、`go mod verify`、`gofmt -l` | 通過；既有 Go 檔案無格式差異 |
| 三個 `.command` 腳本的 `zsh -n` | 通過 |
| `build.command` | 通過，產生 arm64 App |
| `pack.command --no-build --local` | 通過，臨時簽章、DMG 驗證及 SHA-256 校驗通過 |
| 既有前端測試 | 16 項中 15 項通過，1 項因 R9 失敗 |
| 新增的獨立 Go 檢查案例 | 11 項中 4 項通過，7 項預期斷言失敗，證實 6 個不同缺陷；R3 有兩個案例 |
| 額外瀏覽器重現 | 證實 R6 與 R9 的狀態轉換；未出現 JavaScript pageerror |
| `govulncheck` | 標記 9 份標準庫公告的可達符號；見 R2 |
| Git 完整性 | 原始倉庫 exit 8；排除 `._*` 的獨立副本 exit 0 |

**R1：程序目錄比對必須辨識同一個實體目錄。**

位置：[process.go:126](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/process.go#L126)，相關呼叫為 [switch.go:212](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/switch.go#L212)。

`canonicalProcessPath` 解析符號連結後，`selectCodexInstances` 仍直接比較路徑字串。在本機不分大小寫的磁碟上，`Home`／`home`、`Data`／`data` 各自指向相同目錄；測試先以 `os.SameFile` 確認是同一實體，再傳入不同大小寫路徑，函式卻回傳零個目標且沒有錯誤。若使用者以不同大小寫填入這兩個設定，切換流程可能略過應關閉的實例，直接替換其仍在使用的 `auth.json`，也漏掉共用資料目錄衝突。

修正方向：對既有目錄以實體檔案身分比較；不存在的子目錄則解析既有祖先，再按所在檔案系統的規則處理。不能直接把所有路徑轉成小寫，因為專案也可能位於區分大小寫的磁碟。加入不同大小寫、符號連結及不存在子目錄的交叉測試。

重現：`TestDeepReviewCaseAliasesMustSelectRunningInstance`。本次僅使用假程序資料，沒有關閉使用者的 Codex。

**R2：建置工具鏈應更新並重新產生發行檔。**

位置：[build.command:26](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/build.command#L26)。目前腳本使用 PATH 中的 `go`，本機為 `go1.26.1 darwin/arm64`；原有 `dist/CodexSwitch.app/Contents/MacOS/CodexSwitch` 的建置資訊也確認使用 Go 1.26.1。

`govulncheck` 對此環境標記以下 9 份標準庫公告的可達符號：

| 公告 | 標準庫 | 掃描顯示的 1.26 系列修正版本 |
| --- | --- | --- |
| [GO-2026-6090](https://pkg.go.dev/vuln/GO-2026-6090) | crypto/tls | 1.26.6 |
| [GO-2026-5972](https://pkg.go.dev/vuln/GO-2026-5972) | encoding/asn1 | 1.26.6 |
| [GO-2026-5856](https://pkg.go.dev/vuln/GO-2026-5856) | crypto/tls | 1.26.5 |
| [GO-2026-5039](https://pkg.go.dev/vuln/GO-2026-5039) | net/textproto | 1.26.4 |
| [GO-2026-5037](https://pkg.go.dev/vuln/GO-2026-5037) | crypto/x509 | 1.26.4 |
| [GO-2026-4947](https://pkg.go.dev/vuln/GO-2026-4947) | crypto/x509 | 1.26.2 |
| [GO-2026-4946](https://pkg.go.dev/vuln/GO-2026-4946) | crypto/x509 | 1.26.2 |
| [GO-2026-4870](https://pkg.go.dev/vuln/GO-2026-4870) | crypto/tls | 1.26.2 |
| [GO-2026-4866](https://pkg.go.dev/vuln/GO-2026-4866) | crypto/x509 | 1.26.2 |

這是保守的靜態呼叫分析，未逐一證實本 App 在 macOS 上滿足每份公告的攻擊條件。例如憑證驗證公告涉及特定信任鏈條件，部分 TLS 公告涉及伺服端處理。不能將結果解讀成已證實 9 條可利用攻擊路徑。

建議保留 1.26 系列時更新至本次檢查日已發布的 1.26.8，重新建置與掃描，並在建置流程檢查允許的工具鏈版本。只修改 `go.mod` 的最低語言版本，或只更新開發機而不重新發布 App，都不會修補已產生的二進位檔。[Go 官方發布紀錄](https://go.dev/doc/devel/release)列有 1.26.8 與相關修正版本。

**R3：帳號存檔不能信任既有 `.bak` 檔案。**

位置：[app.go:145](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/app.go#L145)。

`os.WriteFile(bak, data, 0600)` 的 `0600` 只在新建檔案時生效。若 `accounts.json.bak` 已存在且為 `0644`，存檔成功後正式憑證檔仍為 `0644`。若 `.bak` 是指向另一檔案的符號連結，WriteFile 會截斷該檔案，再把符號連結 rename 為正式帳號路徑；實測無關檔案被改寫而函式回傳成功。

前者的可讀範圍取決於父目錄權限；預設新建 `0700` 目錄會限制其他使用者。後者需要本機能預先控制 `.bak`，不是已證實的遠端攻擊。兩者都違反憑證檔安全替換的預期。

修正方向：使用同目錄、獨占建立且不可預測名稱的暫存檔，確保 `0600`、同步寫入後才 rename。既有失敗備份應檢查或保留，不能直接跟隨／截斷。現有 `writePrivateFile` 已有可參考的安全替換流程。

重現：`TestDeepReviewStoreMustReplacePermissiveBackup`、`TestDeepReviewStoreMustNotFollowBackupSymlink`。

**R4：空白帳號檔會造成 App 在首次讀取帳號時崩潰。**

位置：[app.go:73](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/app.go#L73)。

當 `accounts.json` 存在但長度為零，`ReadFile` 回傳 `err == nil`，卻因 `len(data) > 0` 不成立而進入 `else if`。`errors.Is(nil, os.ErrNotExist)` 為 false，最終回傳 `(nil, nil)`。`main` 因未取得錯誤而繼續建立 manager，後續 `store.list()` 對 nil store 存取而 panic。這可能出現在使用者建立空檔或檔案遭截斷後。

修正方向：分開處理讀取失敗、空檔、JSON 解析失敗；空檔應回報明確錯誤或返回有效的空 store，不能返回 nil store 加 nil error。重現：`TestDeepReviewEmptyStoreMustReturnStoreOrError`。

**R5：兩個 CodexSwitch 實例會遺失彼此新增的帳號。**

位置：[app.go:72](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/app.go#L72)、[app.go:140](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/app.go#L140)。

每個 store 只在初始化時讀檔，`sync.Mutex` 也只保護該 store 的記憶體。兩個 App 實例同時使用相同資料目錄時，各自保存舊快照；第一個新增 A、第二個再新增 B，兩次操作都成功，但重新開啟後只剩 B。固定 `.bak` 名稱也不能提供跨程序交易保護。測試以兩個獨立 store 模擬兩個程序持有相同檔案的情境，無須真正同時寫入即可重現資料遺失。

修正方向：依資料目錄實作單一實例鎖，或對讀取最新狀態、套用變更及寫回建立完整跨程序鎖定交易。重現：`TestDeepReviewIndependentStoresMustNotLoseAccounts`。

**R6：模態對話框沒有隔離背景鍵盤操作。**

位置：[index.html:433](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/web/index.html#L433)、[index.html:451](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/web/index.html#L451)。

`showDialog` 沒有移動／限制焦點，背景按鈕也沒有設為 inert。列表有兩個帳號時，點第一列「移除」，焦點仍停留在背景刪除按鈕；依序按四次 Tab 可到第二列「套用」，按 Enter 後，`handleApplyAccount` 只檢查 `applyBusy`，直接覆蓋原本的移除對話框及其 onclick callback。取消新對話框後，原移除 Promise 永遠沒有 resolve，`removeBusy` 持續為 true，後續移除操作全部失效，必須重啟。

修正方向：對話框開啟時設定背景 inert、將焦點移入對話框並在關閉後還原；同時讓所有共用對話框入口使用一致的互斥／排隊機制，避免替換尚未完成的 Promise。

實際操作紀錄位於 `browser-reproductions.log`：標題由「移除帳號」變成「套用 Codex 帳號」，取消後 `removalCanReopen=false`、`removeBusy=true`。

**R7：檔案交易接受重複路徑，會靜默覆蓋自身輸出。**

位置：[switch.go:266](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/switch.go#L266)、[switch.go:306](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/switch.go#L306)。

`replaceFiles` 沒有確認所有目的路徑互異。當重複目的檔尚不存在，兩份資料都能暫存，commit 後一份直接覆蓋前一份，整個交易仍回報成功。產品中的具體觸發方式是第一次套用前，將 `CODEX_SWITCH_ENV_FILE` 指向 `active-profile.json`；目前的路徑檢查只排除 auth／accounts 檔，漏掉 profile。結果回傳的 env_path 實際是 JSON，無法作為 Shell 環境檔載入。若重複檔已存在，則會因重複建立固定備份而提前失敗，行為不一致。

修正方向：交易入口統一拒絕重複及實體別名目的路徑，並把 auth、env、profile、accounts 的衝突檢查放在關閉 Codex 前。重現：`TestDeepReviewNewDuplicateTransactionTargetMustBeRejected`。

**R8：路徑末端合法的 `~` 被誤判。**

位置：[settings.go:24](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/settings.go#L24)。

展開 `~/...` 後，只要最終字串以 `/~` 結尾，就被改成家目錄。測試輸入 `~/codex-review-profile/~`，預期保留名為 `~` 的子目錄，實際得到使用者家目錄。作為 CODEX_HOME 時會改變登入檔的寫入位置。

修正方向：在展開之前分別處理精確值 `~` 與前綴 `~/`，不要以展開後的尾端字串判斷。重現：`TestDeepReviewLiteralTildeDirectoryMustRemainLiteral`。

**R9：前端回歸測試的更新初始狀態已過時。**

位置：[review.browser.cjs:19](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/tests/review.browser.cjs#L19)、[review.browser.cjs:42](https://github.com/VaderChen/CodexSwitch/blob/2eabe4d0f23549c1860812a15e92b692a618c503/src/tests/review.browser.cjs#L42)。

頁面啟動已會自動呼叫 `checkForUpdate`，fixture 第一次呼叫就設為 available，因此打開「關於」時按鈕已經是「下載並安裝」。測試卻把第一次 click 當成「檢查更新」，按下後實際進入 downloading，再等「下載並安裝」文字出現，30 秒後逾時。這項失敗不能當作已證實的產品下載錯誤。

在暫存副本中只調整 fixture，使啟動檢查回 current、第二次手動檢查回 available，並將總查詢次數預期改為兩次，同一瀏覽器測試立即通過；產品 JavaScript 完全未改。正式測試應分別覆蓋啟動發現更新及手動發現更新兩種流程。

**R10：Git 倉庫被 AppleDouble 側錄檔污染。**

位置：`.git/refs/**/._*` 與 `.git/objects/**/._*`。

`git fsck --full` 回傳 8，包含 177 行 `error:`／`bad sha1 file:` 訊息。另建立排除 `._*` 的 Git 副本後，同一檢查回傳 0，只剩不影響完整性的 dangling tree 提示。這證明本次錯誤來自 macOS 中繼資料檔，並未發現真正 Git 物件損壞；前次搬移時已確認來源也有相同情況。

修正方向：先備份 `.git`，清理其內 AppleDouble 側錄檔並重跑 fsck；一般 `.gitignore` 無法保護 Git 自己的內部目錄。不要刪除正常 refs／objects，也不要把 dangling tree 當成損壞而直接移除。本次保留原倉庫未清理。

既有實作中已驗證的保護包括：OAuth state 與 PKCE、登入取消不落盤、匯入先完整驗證再提交、匯出原子替換且不跟隨目標 symlink、重置 request ID 持久保存、下載大小與 SHA-256 校驗、更新簽署團隊／版本檢查，以及 updater helper 的啟動失敗還原。原測試與額外模擬案例可驗證其中的函式行為；正式 Gatekeeper 與公證服務的端到端流程未在本次執行。

測試缺口集中在主流程：基線 OAuth `start`／`complete`、`replaceFiles`、實際程序掃描、更新 `install`／`stageAndLaunchUpdate` 的 statement coverage 為 0%，`useAccount` 為 6.8%。本次另補模擬 OAuth callback／交換／取消與檔案交易案例做檢查，但尚未整合進正式測試集。Chromium 上的 DOM 測試不能取代原生 WKWebView 與 AppKit 驗收；govulncheck 也不能涵蓋 Cgo/C++ 與系統 WebKit 的所有安全問題。建置另出現第三方 webview 的 C++ literal operator 棄用警告，以及 hdiutil 建立映像命令的棄用提示，本次皆未阻擋建置。

線上服務、Developer ID 公證及覆蓋已安裝 App 的自動更新不在本次隔離測試範圍內。新版未回報 ready 時，現有 helper 保留舊備份而不自動換回，屬現有測試明確接受的行為；仍應在正式發行前驗收手動復原程序。

建議修正順序為 R1／R2，接著 R3–R7，再處理 R8–R10 與主要測試缺口。

基線測試與為證明缺陷而新增的預期失敗案例分開驗證；文中原始碼行號連結固定至檢查時的 commit。

正式回歸測試已整合至 `src/*_test.go` 與 `src/tests/`；目前測試方式見[專案 README](../../README.md)，修正結果見 [第一輪修正](UPDATE.md)及[後續修正](FOLLOWUP.md)。
