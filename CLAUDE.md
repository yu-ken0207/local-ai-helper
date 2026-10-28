# local-ai-helper

Local AI 電腦小幫手（Go）。用法、結構、進度看 [README.md](README.md)。手機 App 是另一個 repo：local-chat。

- Go 裝在 `~/.local/go`（`export PATH=$HOME/.local/go/bin:$PATH`）；`go test ./...`、`scripts/build.sh` 編 Windows／Linux。
- 每完成一個階段就 commit 並 push（使用者已授權）。
- 裝了企業防毒的電腦會刪掉沒簽章的測試執行檔：**不要把 exe 丟到那種電腦測**，也不要嘗試繞過防毒；Windows 實測等使用者指定測試機。
