# Local AI 小幫手（local-ai-helper）

裝在家裡電腦上的小程式，讓全家的手機（Local AI App，[local-chat](https://github.com/yu-ken0207/local-chat)）用這台電腦上的 AI 模型，不用付錢買雲端。支援 Windows 與 Linux。

## 使用者看到的

- 點兩下打開：瀏覽器跳出「Local AI 小幫手」管理頁（只有這台電腦看得到），系統匣有圖示可以再打開或結束
- 管理頁：這台電腦的名稱（可改）、手機連線位址；之後會有配對新手機、已配對的手機、模型
- 已經開著再點一次：只會把管理頁叫出來，不會開第二個

## 設計

- 手機只跟小幫手講話（要帶配對時拿到的權杖），小幫手再接後面的引擎：Ollama（預設，沒有就自動安裝）、LM Studio、OpenAI 格式的伺服器（llama.cpp、vLLM、Jan…）
- 小幫手對手機說 Ollama 的格式；引擎維持只給本機用，不用打開網路分享
- 兩個連接埠：11480 給手機（區網）、11481 是管理頁（只聽 127.0.0.1）
- 管理頁擋 DNS rebinding（主機名稱一定要是 127.0.0.1／localhost）與跨網站送出（會改東西的請求要帶 `X-Local-AI: 1`）
- 設定與記錄在使用者設定資料夾：Windows `%AppData%\local-ai-helper`，Linux `~/.config/local-ai-helper`（`config.json`、`helper.log`）；手機權杖只存雜湊

## 開發

```bash
export PATH=$HOME/.local/go/bin:$PATH
go test ./...
scripts/build.sh          # dist/ 下產生 windows-amd64.exe、linux-amd64、linux-arm64
./dist/local-ai-helper-linux-arm64 --no-browser --no-tray --config-dir /tmp/x   # 沒有桌面時
```

```
cmd/local-ai-helper/   進入點：只開一個、開管理頁、系統匣、結束
internal/config/       設定檔（識別碼、名稱、連接埠、已配對的手機）
internal/admin/        本機管理頁與 API（page/index.html 內嵌，中英文跟著瀏覽器）
internal/netinfo/      找手機連得到的位址（排除 Docker、WSL 等虛擬網卡）
internal/platform/     開瀏覽器、介面語言、有沒有桌面
internal/tray/         系統匣圖示
```

## 進度

| 步驟 | 內容 | 狀態 |
|---|---|---|
| 1 | 骨架：管理頁、設定檔、系統匣、Windows／Linux 執行檔 | 完成（Linux 實測過；Windows 待測試機） |
| 2 | 配對（QR code＋6 位數配對碼）與門口，先接 Ollama | |
| 3 | App：掃 QR code 配對、找小幫手、輸入配對碼 | |
| 4 | 接 LM Studio 與 OpenAI 格式的伺服器 | |
| 5 | App：依引擎隱藏做不到的功能、在手機下載模型與推薦 | |
| 6 | 一鍵安裝 Ollama、偵測硬體 | |
| 7 | 開機自動啟動、使用中不睡眠、防火牆 | |
| 8 | App：連不到時說得更準、第一次載入的等待說明 | |

## 已知問題

- **沒有程式碼簽章，防毒軟體可能把執行檔當成可疑程式刪掉**（裝了企業防毒的電腦就刪過測試版）。要給一般人用之前要處理：簽章、送防毒廠商誤判審查。
