# CodexSwitch

CodexSwitch 是 macOS 上的 Codex 帳號管理工具，讓你在多個 Codex 帳號之間快速切換。

## 操作介面

<img src="images/cap001.jpg" alt="CodexSwitch 操作介面" width="480">

## 功能與特色

- 管理多個 Codex 帳號
- 一鍵套用指定帳號
- 套用前只關閉相同 `CODEX_HOME` 的 Codex App，保留其他獨立實例
- 匯入與匯出帳號列表
- 偵測所有執行中的 Codex 實例，依帳號與目錄分別標示使用中，可同時顯示多個勾選
- 顯示帳號連結狀態與下一次自動重置時間
- 背景更新 **帳號剩餘用量**，以「五小時 / 七天」百分比呈現，缺少資料顯示 `-`
- 支援 **用量重置操作**
- 支援 Codex 環境設定與帳號資料安全保存
- 按鈕說明泡泡可隨時開關
- 記住視窗尺寸與位置，支援 macOS 快捷鍵
- 明亮、簡潔、緊湊的介面

## 快速編譯與開始

雙擊 `run.command` 即可啟動 CodexSwitch。第一次使用時，按下「開啟瀏覽器登入」完成帳號登入。

在帳號列表中按「套用」，即可切換到指定帳號。CodexSwitch 會先確認 Codex App 狀態，完成後啟動對應帳號。

## 下載與更新

從 [最新 Release](https://github.com/VaderChen/CodexSwitch/releases/latest) 下載 DMG，開啟後將 CodexSwitch 拖入「應用程式」。目前發布 Apple Silicon（`arm64`）版本。

DMG 檔名格式為 `CodexSwitch-1.YY.MMDD-build-HHmm-arm64.dmg`，例如 `CodexSwitch-1.26.0916-build-0917-arm64.dmg`。正式版本經 Developer ID 簽署與 Apple 公證，Release 同時提供 `PACKAGES-SHA256SUMS` 校驗檔。

App 啟動後會在背景自動檢查更新，有新版時在主畫面提示；也可從「關於」對話框手動檢查。下載時顯示百分比、已下載大小及進度條，關閉「關於」仍可在主畫面查看。私人倉庫需先透過 `gh auth login` 登入有讀取權限的 GitHub 帳號。

若前次更新留下備份，會在確認目前 App 簽章後，將舊備份保留於 App 同層的 `.codexswitch-backup-*` 目錄，再繼續更新；剛建立的備份需等待 90 秒，避免與前次安裝程序衝突。

「關於」的紅字「強制更新」會直接下載並安裝最新正式版，即使版本相同或目前為較新的本機版也會重裝。下載校驗、簽章與 Apple 安全驗證仍會執行。

## 帳號用量與設定

- 帳號列日期顯示五小時、七天用量中，最近一次即將到來的自動重置時間；沒有有效時間時顯示 `-`。
- 將游標移至帳號，可查看兩種用量各自的自動重置時間。
- 用量重置需長按 2 秒，會使用帳號可用的重置次數。
- 齒輪設定可指定每個帳號的 `CODEX_HOME` 與 `USER_DATA_DIR`；留空會填入系統預設值，下次套用生效。

## 移除帳號

按帳號列右側的「×」，會顯示包含帳號名稱的確認對話框。按「確認移除」才會刪除 CodexSwitch 儲存的該帳號登入資料；按「取消」或 Escape 可返回。移除後列表會更新，連續點擊不會重複刪除；失敗時顯示錯誤並允許重試。

此操作只移除 CodexSwitch 的帳號紀錄，不會登出已執行的 Codex，也不會刪除該帳號設定的目錄。

## 多實例切換

- 每個獨立 Codex 實例應使用不同的 `CODEX_HOME` 與 `USER_DATA_DIR`。
- 按「偵測目前帳號」會讀取所有執行中的實例；帳號及兩個目錄皆符合時顯示勾選，並停用該列的「套用」。
- 套用時只關閉相同 `CODEX_HOME` 的實例；沒有對應實例時直接啟動，不需要先開啟 Codex。
- 共用 `CODEX_HOME` 的實例會一起關閉，避免同時讀寫被替換的登入憑證。
- 不同 `CODEX_HOME` 若共用同一個 `USER_DATA_DIR`，會停止套用並提示設定獨立目錄。
- 若無法確認對應程序身分或讀取其環境，會提示處理，不會任意關閉其他實例。

## 建置與封裝

macOS 開發環境需安裝 Go 與 Xcode Command Line Tools。執行 `./build.command` 產生 `dist/CodexSwitch.app`；建置前會清空 `dist`。

`pack.command` 為本機維護的簽署與封裝腳本，不包含在 GitHub 倉庫。發布 DMG 放在 `dist`，打包完成後移除中繼資料。從 GitHub 取得原始碼後，請使用 `build.command` 建置，或直接下載 Release。
