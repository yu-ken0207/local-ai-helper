# local-ai-helper

Local AI 電腦小幫手（Go）。用法、結構、進度看 [README.zh-TW.md](README.zh-TW.md)。手機 App 是另一個 repo：local-chat。

- README.md 是給外國人看的英文版，README.zh-TW.md 是中文版（含進度）；功能有變時兩份都要改。
- Go 裝在 `~/.local/go`（`export PATH=$HOME/.local/go/bin:$PATH`）；`go test ./...`、`scripts/build.sh` 編 Windows／Linux。
- 每完成一個階段、測試過就 commit；push 前要列出還沒 push 的 commit 文字給使用者看，等使用者說「確認完成」才 push（2026-10-29 改）。
- 裝了企業防毒的電腦會刪掉沒簽章的測試執行檔：**不要把 exe 丟到那種電腦測**，也不要嘗試繞過防毒；Windows 實測等使用者指定測試機。
