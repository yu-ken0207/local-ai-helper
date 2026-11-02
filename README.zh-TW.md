# Local AI 小幫手（local-ai-helper）

[English](README.md) | **繁體中文**

[![Tests](https://github.com/yu-ken0207/local-ai-helper/actions/workflows/test.yml/badge.svg)](https://github.com/yu-ken0207/local-ai-helper/actions/workflows/test.yml) [![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

裝在家裡電腦上的小程式，讓全家人的手機透過 Local AI App（[local-chat](https://github.com/yu-ken0207/local-chat)）使用這台電腦上的 AI 模型，不用付費訂閱雲端服務。支援 Windows 與 Linux。

## 怎麼開始

目前沒有提供編譯好的執行檔（還沒有程式碼簽章，防毒軟體可能誤判），需要自己編譯：

1. 裝 [Go](https://go.dev/dl/)（版本見 `go.mod`）
2. 下載並執行：
   ```bash
   git clone https://github.com/yu-ken0207/local-ai-helper.git
   cd local-ai-helper
   go run ./cmd/local-ai-helper
   ```
   也可以用 `scripts/build.sh` 編譯出 Windows 與 Linux 的執行檔（放在 `dist/`），之後點兩下就能打開。
3. 瀏覽器會打開管理頁（`http://127.0.0.1:11481`）。電腦上還沒有 Ollama 的話，按「安裝 Ollama」。
4. 在管理頁按「配對新手機」，手機打開 [Local AI](https://github.com/yu-ken0207/local-chat) 掃畫面上的 QR code。手機和電腦要在同一個 Wi-Fi，或兩邊都開 Tailscale。

沒有桌面的 Linux：`go run ./cmd/local-ai-helper --no-browser --no-tray`，管理頁可以透過 SSH 通道轉送 11481 埠來開。

## 使用方式

- 點兩下打開：瀏覽器跳出「Local AI 小幫手」管理頁（只有這台電腦看得到），系統匣有圖示可以再打開或結束
- 管理頁：配對新手機（QR code＋6 位數配對碼，開放 5 分鐘）、已配對的手機（最近使用、移除）、AI 引擎狀態、這台電腦的名稱（可改）、手機連線位址
- 已經開著再點一次：只會把管理頁叫出來，不會開第二個
- 開機自動打開（預設開，管理頁「這台電腦」可以關）；開機打開時不跳出管理頁
- 手機在聊天、載入模型、下載時，電腦不會睡著（用完後再保持 10 分鐘；螢幕照常會關）
- 防火牆擋住手機時：Windows 按「允許手機連進來」（跳出系統確認，只放行家用與公司網路）；Linux 顯示一行可以複製的指令（要 sudo，小幫手不代勞）；Windows 把網路當成「公用網路」時教使用者改成私人網路。區網的手機連進來過，提示就消失

## 設計

- 手機只連小幫手（要帶配對時拿到的權杖），再由小幫手轉給後面的引擎：Ollama（預設，沒有的話可以一鍵安裝）、LM Studio、相容 OpenAI API 的伺服器（llama.cpp、vLLM、Jan…）
- 一鍵安裝 Ollama（管理頁「安裝 Ollama」）：不用官方安裝程式（Windows 會多一個系統匣圖示、Linux 要 sudo），改下載 GitHub 上官方發佈的壓縮檔（Linux `.tar.zst`、Windows `.zip`，約 1.6 GB），**比對官方 `sha256sum.txt`** 後解到使用者資料夾（Windows `%LOCALAPPDATA%\local-ai-helper\ollama`、Linux `~/.local/share/local-ai-helper/ollama`），不用管理員權限。先解到旁邊的資料夾、全部成功才換上去；會擋下解開後路徑或連結指到資料夾外面的檔案；磁碟要有壓縮檔三倍的空間。Jetson 與 AMD 顯示卡（Linux）照官方 install.sh 另外加裝 jetpack／rocm 套件
- 小幫手裝的 Ollama 由小幫手負責執行（`ollama serve`，輸出寫到設定資料夾的 `ollama.log`）：小幫手啟動時自動開、意外結束 3 秒後重開（一分鐘內連續失敗 5 次就停，管理頁顯示原因與「打開 Ollama」）、小幫手結束一起關；Linux 用 Pdeathsig、Windows 用 Job Object，小幫手當掉也不會留下 Ollama。使用者自己裝的 Ollama 不會被蓋掉（沒開時管理頁有「打開 Ollama」；Windows 打開它自己的 `ollama app.exe`）
- 開機自動啟動：Windows 寫使用者自己的 `HKCU\...\Run`（不用管理員；工作管理員的「啟動」看得到）；Linux 寫 `~/.config/autostart/local-ai-helper.desktop`（桌面登入時打開）。每次啟動都重寫，執行檔搬了位置也跟著改；用 `--config-dir` 啟動時（測試用）不會改開機設定
- 使用中不睡眠：聊天、預先載入、下載期間不讓電腦睡著，用完後再保持 10 分鐘。Windows 用 `SetThreadExecutionState`（固定在同一條執行緒）；Linux 在背景開 `systemd-inhibit --what=sleep:idle`，不會拖慢聊天；如果它因為沒權限自己結束了，管理頁會說明
- 防火牆：Linux 看 `/etc/ufw/ufw.conf` 與 firewalld 有沒有在跑（一般使用者讀不到 ufw 規則，只能說「不確定」）；Windows 用 `netsh` 看有沒有「Local AI Helper」規則、PowerShell 看網路類型。放行時先刪掉這個程式的其他規則（第一次開時在 Windows 的提示按取消會留下封鎖規則，封鎖比放行優先）。有區網的手機（不算電腦自己）連進來過就當作沒擋
- 系統沒有根憑證時（精簡的 Linux）用 Go 內建的一份（`x509roots/fallback`），下載才連得上 HTTPS
- 引擎：Ollama 原樣轉送；LM Studio 用原生 API 列模型（0.4 起 `/api/v1/models`，舊版 `/api/v0/models`）與載入／卸載，聊天走 OpenAI 格式；其他 OpenAI 格式伺服器在常見連接埠（8080、8000、1337、4891、5000）自動偵測（看 `/v1/models` 的格式與 `owned_by`），也可以在管理頁手動加（網址、名稱、API 金鑰）
- 模型清單合併所有引擎，每個模型多 `engine`、`engine_name`、`can_load`（能不能預先載入）；跟 Ollama 撞名的改叫「名稱@引擎」（Ollama 的名稱永遠不變）；聊天時依名稱找回引擎，OpenAI 的串流（含 `reasoning_content`、用量）轉回 Ollama 的逐行格式；圖片轉成 `image_url`
- 下載模型在小幫手這邊跑（不綁手機的連線），手機鎖螢幕、App 關掉都不會中斷，回來再問進度：Ollama 用 `/api/pull`（各層進度加總，可以取消）；LM Studio 用 `/api/v1/models/download`＋每秒問進度（LM Studio 沒有取消的 API）。下載工作只存在記憶體裡，結束後保留 1 小時
- 硬體：記憶體（Linux `/proc/meminfo`、Windows `GlobalMemoryStatusEx`）＋`nvidia-smi` 的顯示卡與顯示記憶體（Windows 執行時不跳黑色視窗）；顯示記憶體回 `[N/A]` 的（DGX Spark、Jetson）當作跟主記憶體共用。推薦哪些模型由 App 判斷
- 小幫手對手機提供 Ollama 格式的 API；引擎只需要開在本機，不用打開網路分享
- 兩個連接埠：11480 給手機（區網）、11481 是管理頁（只聽 127.0.0.1）
- 管理頁擋 DNS rebinding（主機名稱一定要是 127.0.0.1／localhost）與跨網站送出（會改東西的請求要帶 `X-Local-AI: 1`）
- 設定與記錄在使用者設定資料夾：Windows `%AppData%\local-ai-helper`，Linux `~/.config/local-ai-helper`（`config.json`、`helper.log`）；手機權杖只存雜湊

## 手機 App 怎麼連（API）

小幫手對手機提供 Ollama 格式的 API，另外加幾個自己的 API。錯誤一律是 `{"error": "說明", "code": "代碼"}`。

| API | 權杖 | 說明 |
|---|---|---|
| `GET /helper/v1/hello` | 不用 | `{app:"local-ai-helper", id, name, version, pairing}`；App 掃區網時用來認出小幫手 |
| `POST /helper/v1/pair` | 不用 | `{code, deviceName}` 換 `{token, deviceId, helper:{id,name}}`；錯誤代碼 `not_pairing`（409）、`bad_code`（403）、`too_many_attempts`（429，打錯 5 次時段就關掉） |
| `GET /helper/v1/info` | 要 | 電腦名稱、引擎狀態 `engines[]`、可用功能 `features`（`pull`、`preload`）、可以下載的引擎 `download_engines`（第一個是預設，Ollama 優先）、配備 `hardware`（`memory_bytes`、`gpus[{name, vram_bytes}]`、`unified`） |
| `POST /helper/v1/downloads` | 要 | `{model, engine?}` 開始下載（202），回傳工作；同一個模型下載中再按回同一個工作；沒有能下載的引擎回 409 `no_download_engine` |
| `GET /helper/v1/downloads` | 要 | `{downloads:[{id, model, engine, status, completed, total, code, error, cancellable}]}`；`status` 是 `downloading`／`done`／`failed`／`cancelled`；名稱打錯 `code` 是 `not_found` |
| `DELETE /helper/v1/downloads/{id}` | 要 | 下載中的取消（LM Studio 回 501 `unsupported`）；已經結束的從清單拿掉 |
| `/api/version` `tags` `show` `ps` `chat` `generate` `pull` | 要 | Ollama 格式；依模型轉給對的引擎（邊收邊送）。`tags` 合併所有引擎；`generate` 只用來預先載入（`keep_alive`）與卸載（`keep_alive: 0`），OpenAI 格式伺服器回 501 `unsupported`；`pull` 原樣轉給 Ollama（App 改用上面的下載 API）。其他 Ollama API（刪除、建立、推送）回 404 `not_allowed`；一個引擎都沒有回 502 `no_engine`。`chat` 可以帶工具（`tools`、`tool_calls`、`role: tool`，手機上動手做事用）：Ollama 原樣轉送；LM Studio、OpenAI 格式的伺服器由小幫手翻譯，串流的工具呼叫拼完整再一次給（arguments 是物件，拼起來不是合法 JSON 就原樣給字串），工具結果依 `tool_call_id`、沒有時依工具名稱與順序配對。`show` 的 capabilities 有 `tools` 才代表這個模型會叫工具（LM Studio 看 `trained_for_tool_use`；OpenAI 格式的伺服器查不到，當作不會） |

- 權杖放在 `Authorization: Bearer ...`；沒帶或被移除回 401 `not_paired`（App 要請使用者重新配對）
- QR code 內容：`localai://pair?v=1&id=...&name=...&port=11480&code=123456&ip=192.168.1.20&ip=...`（App 依序試每個位址的 hello，識別碼要對）
- 一個配對時段只配一支手機，配好就關；權杖只回傳這一次，小幫手只存雜湊

## 開發

```bash
go test ./...
scripts/build.sh          # dist/ 下產生 windows-amd64.exe、linux-amd64、linux-arm64
./dist/local-ai-helper-linux-arm64 --no-browser --no-tray --config-dir /tmp/x   # 沒有桌面時
# 測一鍵安裝又不想真的下載：LOCAL_AI_HELPER_OLLAMA_RELEASES 指到假的發佈頁（要有 /latest/download/sha256sum.txt 與壓縮檔），
# XDG_DATA_HOME 換掉安裝位置、PATH 拿掉 ollama
```

```
cmd/local-ai-helper/   進入點：只開一個、開管理頁、系統匣、結束
internal/config/       設定檔（識別碼、名稱、連接埠、已配對的手機）
internal/admin/        本機管理頁與 API（page/index.html 內嵌，中英文跟著瀏覽器）
internal/gateway/      手機連線的入口：hello、配對、權杖檢查、依模型轉給對的引擎、下載工作（downloads.go）
internal/engine/       LM Studio、OpenAI 格式伺服器：偵測、模型清單、聊天格式轉換、載入／卸載、LM Studio 下載（enginetest/ 是假引擎）
internal/hardware/     記憶體、NVIDIA 顯示卡
internal/system/       開機自動啟動、使用中不睡眠、防火牆（Windows／Linux 各自的實作）
internal/ollama/       一鍵安裝 Ollama（下載、比對官方檢查碼、解開）與開著它（自動重開）
internal/pairing/      配對時段、配對碼、權杖（只存雜湊）、QR code
internal/netinfo/      找手機連得到的位址（排除 Docker、WSL 等虛擬網卡）
internal/platform/     開瀏覽器、介面語言、有沒有桌面、子程式（不跳視窗、跟著小幫手結束）、磁碟空間、資料資料夾
internal/tray/         系統匣圖示
```

## 進度

| 步驟 | 內容 | 狀態 |
|---|---|---|
| 1 | 骨架：管理頁、設定檔、系統匣、Windows／Linux 執行檔 | 完成（Linux 實測過；Windows 待測試機） |
| 2 | 配對（QR code＋6 位數配對碼）與門口，先接 Ollama | 完成（Linux 實機測試：配對、聊天串流、從另一台 Windows 電腦經 Tailscale 連入） |
| 3 | App：掃 QR code 配對、找小幫手、輸入配對碼 | 完成（local-chat） |
| 4 | 接 LM Studio 與 OpenAI 格式的伺服器 | 完成（Linux 實機測試 LM Studio 0.4 llmster：合併清單、看圖聊天、載入／卸載） |
| 5 | App：依引擎隱藏做不到的功能、在手機下載模型與推薦（硬體偵測提前在這步做） | 完成（Linux 實機測試：Ollama 下載進度與取消、LM Studio 下載進度、名稱打錯；配備回報 GB10 共用記憶體） |
| 6 | 一鍵安裝 Ollama | 完成（Linux 乾淨容器實測：下載 1.6 GB、比對檢查碼、解開、開起來、偵測到 GB10、跑模型；強制結束會自動重開、小幫手重開會自動開 Ollama；管理頁每個狀態截圖確認。Windows 待測試機） |
| 7 | 開機自動啟動、使用中不睡眠、防火牆 | 完成（Linux 實機測試：開機啟動檔建立與關掉、ufw 偵測與放行指令、聊天時擋睡眠在非桌面登入下被拒並正確說明；管理頁截圖確認。Windows 待測試機） |
| 8 | App：連不到時說得更準、第一次載入的等待說明 | 完成（local-chat 階段 J） |
| 9 | 手機上動手做事 K1：轉送與翻譯工具、標出會叫工具的模型（設計見 local-chat `docs/設計_手機上動手做事.md`） | 完成（假引擎測試；Linux 實機測試 Ollama gemma4:12b 叫工具） |

## 已知問題

- **Linux 有開防火牆（ufw）時，區網的手機連不進 11480**（例如 11434 有放行、11480 沒有；Tailscale 不受影響）。管理頁會顯示放行指令，要使用者自己執行（要 sudo）。

- **Linux 的「使用中不睡眠」只在從桌面登入後開啟小幫手時有效**（開機自動打開就是這種）；從 SSH 或背景開的會被系統拒絕（管理頁會說明）。

- **手機沒辦法喚醒睡著的電腦**：需要網路喚醒（Wake-on-LAN，要先在 BIOS 打開），還沒做。

- **LM Studio 從 Hugging Face 下載，很慢**（實測約 0.3 MB/s；Ollama 同一台約 28 MB/s）。LM Studio 沒有取消下載的 API，要取消只能在 LM Studio 裡按。

- **Ollama 上已經有的模型再下載一次，如果官方出了新版會下載新版**（實測 `gemma4:e2b`）。App 的推薦清單把已經有的標成「已下載」、不給再按，不會誤觸。

- **Windows 的 AMD 顯示卡還沒有 rocm 套件**（只裝主程式，用 CPU 或 Vulkan）。

- **精簡的 Linux 沒有根憑證時，Ollama 自己下載模型會失敗**（小幫手有內建的一份，Ollama 沒有）；一般桌面系統都有，裝 `ca-certificates` 後在管理頁重開 Ollama 就好。

- **沒有程式碼簽章，防毒軟體可能把執行檔當成可疑程式刪掉**（裝了企業防毒的電腦就刪過測試版）。要給一般人用之前要處理：簽章、送防毒廠商誤判審查。

## License

Apache-2.0，見 [LICENSE](LICENSE)。用到的第三方套件（fyne.io/systray、klauspost/compress、skip2/go-qrcode、golang.org/x/sys、golang.org/x/crypto、godbus/dbus）都是 Apache-2.0、MIT 或 BSD 授權。
