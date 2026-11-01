# Local AI Helper (local-ai-helper)

**English** | [繁體中文](README.zh-TW.md)

[![Tests](https://github.com/yu-ken0207/local-ai-helper/actions/workflows/test.yml/badge.svg)](https://github.com/yu-ken0207/local-ai-helper/actions/workflows/test.yml) [![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

A small desktop program that lets every phone at home use the AI models on one computer, through the [Local AI](https://github.com/yu-ken0207/local-chat) iPhone app. No cloud and nothing to pay for. Windows and Linux.

## What it does

- **Pairing**: a phone pairs by scanning a QR code (or entering a 6-digit code). Each phone gets its own token, and the helper stores only a hash of it.
- **One gateway for several engines**: chats go to Ollama, LM Studio, or any OpenAI-compatible server (llama.cpp, vLLM, Jan…), and their model lists are merged. The phone always speaks the Ollama API.
- **One-click Ollama install, no admin rights**: downloads the official release archive, checks it against the official `sha256sum.txt`, unpacks it into your user folder, and keeps it running (restarts it if it crashes).
- **Stays awake while in use**: the computer won't sleep while a phone is chatting, loading a model, or downloading one (the screen still turns off).
- **Firewall help**: detects when a firewall blocks phones. On Windows one button opens it (with the system prompt); on Linux it shows a command to copy.
- **Downloads from the phone**: model downloads run on the computer, so they continue when the phone is locked or the app is closed.
- **Starts at login** (can be turned off) and has a tray icon.

## Getting started

There are no prebuilt binaries yet: they are not code-signed, and antivirus software may flag them. Build it yourself:

1. Install [Go](https://go.dev/dl/) (version in `go.mod`).
2. Download and run:
   ```bash
   git clone https://github.com/yu-ken0207/local-ai-helper.git
   cd local-ai-helper
   go run ./cmd/local-ai-helper
   ```
   Or run `scripts/build.sh` to build Windows and Linux binaries into `dist/`, then double-click to open.
3. The admin page opens in your browser (`http://127.0.0.1:11481`). If the computer has no Ollama yet, click **Install Ollama**.
4. Click **Pair a New Phone** and scan the QR code with the [Local AI](https://github.com/yu-ken0207/local-chat) app. The phone and the computer need to be on the same Wi-Fi, or both on Tailscale.

Linux without a desktop: `go run ./cmd/local-ai-helper --no-browser --no-tray`, and open the admin page through an SSH tunnel to port 11481.

## Security

- Two ports: 11480 for phones (LAN) and 11481 for the admin page, which listens on 127.0.0.1 only.
- The admin page blocks DNS rebinding (the host must be 127.0.0.1 or localhost) and cross-site requests (requests that change something must send `X-Local-AI: 1`).
- The engines stay local; you don't need to expose Ollama to the network.
- A pairing window pairs one phone, stays open for 5 minutes, and closes after 5 wrong codes.
- Only the Ollama APIs the app needs are open; delete, create, and push return 404.

## API for the phone app

The helper speaks the Ollama API to phones, plus a few endpoints of its own. Errors are always `{"error": "message", "code": "code"}`.

| API | Token | Description |
|---|---|---|
| `GET /helper/v1/hello` | No | `{app:"local-ai-helper", id, name, version, pairing}`; the app uses it to recognize helpers on the LAN |
| `POST /helper/v1/pair` | No | `{code, deviceName}` returns `{token, deviceId, helper:{id,name}}`; error codes `not_pairing` (409), `bad_code` (403), `too_many_attempts` (429, the window closes after 5 wrong codes) |
| `GET /helper/v1/info` | Yes | Computer name, engine status `engines[]`, features `features` (`pull`, `preload`), engines that can download `download_engines` (the first is the default, Ollama first), hardware `hardware` (`memory_bytes`, `gpus[{name, vram_bytes}]`, `unified`) |
| `POST /helper/v1/downloads` | Yes | `{model, engine?}` starts a download (202) and returns the job; asking again for a model already downloading returns the same job; 409 `no_download_engine` when no engine can download |
| `GET /helper/v1/downloads` | Yes | `{downloads:[{id, model, engine, status, completed, total, code, error, cancellable}]}`; `status` is `downloading`, `done`, `failed`, or `cancelled`; a misspelled name gives `code` `not_found` |
| `DELETE /helper/v1/downloads/{id}` | Yes | Cancels a running download (LM Studio returns 501 `unsupported`); removes a finished one from the list |
| `/api/version` `tags` `show` `ps` `chat` `generate` `pull` | Yes | Ollama format, routed to the right engine by model name (streamed). `tags` merges all engines. `generate` is only for preloading (`keep_alive`) and unloading (`keep_alive: 0`); OpenAI-compatible servers return 501 `unsupported`. `pull` goes straight to Ollama (the app uses the download API above). Other Ollama APIs (delete, create, push) return 404 `not_allowed`; 502 `no_engine` when there is no engine at all. `chat` supports tools (`tools`, `tool_calls`, `role: tool`): passed through to Ollama, translated for LM Studio and OpenAI-compatible servers (streamed tool calls are assembled and sent once; tool results are matched by `tool_call_id`, or by tool name and order). A model can call tools only if `show` lists `tools` in its capabilities. |

- Send the token as `Authorization: Bearer ...`. A missing or revoked token returns 401 `not_paired` (the app asks the user to pair again).
- QR code content: `localai://pair?v=1&id=...&name=...&port=11480&code=123456&ip=192.168.1.20&ip=...` (the app tries each address's hello in order, and the ID must match).
- A pairing window pairs only one phone and then closes. The token is returned only once; the helper keeps only its hash.

## Development

```bash
go test ./...
scripts/build.sh          # builds windows-amd64.exe, linux-amd64, linux-arm64 into dist/
./dist/local-ai-helper-linux-arm64 --no-browser --no-tray --config-dir /tmp/x   # without a desktop
# To test the Ollama installer without a real download: point LOCAL_AI_HELPER_OLLAMA_RELEASES at a fake
# release page (with /latest/download/sha256sum.txt and the archive), change XDG_DATA_HOME, and remove ollama from PATH
```

```
cmd/local-ai-helper/   entry point: single instance, admin page, tray, exit
internal/config/       config file (ID, name, ports, paired phones)
internal/admin/        local admin page and API (page/index.html is embedded; Chinese or English, following the browser)
internal/gateway/      what phones connect to: hello, pairing, token check, routing by model, download jobs
internal/engine/       LM Studio and OpenAI-compatible servers: detection, model list, chat translation, load/unload
internal/hardware/     memory and NVIDIA GPUs
internal/system/       start at login, stay awake, firewall (Windows and Linux implementations)
internal/ollama/       one-click Ollama install (download, checksum, unpack) and keeping it running
internal/pairing/      pairing window, pairing code, tokens (hash only), QR code
internal/netinfo/      addresses a phone can reach (skips Docker, WSL, and other virtual adapters)
internal/platform/     browser, interface language, desktop detection, child processes, disk space, data folders
internal/tray/         tray icon
```

The development log is in the [Chinese README](README.zh-TW.md).

## Known issues

- Tested on real Linux machines. The Windows build passes the automated tests on GitHub, but has not been tried on a real Windows PC yet.
- **Linux with ufw**: phones on the LAN can't reach port 11480 until you allow it. The admin page shows the command (needs sudo). Tailscale is not affected.
- **Staying awake on Linux** only works when the helper was started from a desktop login (starting at login does this). Started from SSH or in the background, the system refuses, and the admin page explains why.
- **A sleeping computer can't be woken from the phone**: Wake-on-LAN is not done yet.
- **LM Studio downloads from Hugging Face can be slow**, and LM Studio has no API to cancel them.
- **Downloading a model Ollama already has gets the newer version** if the official one was updated. The app marks models you already have as downloaded.
- **Windows with AMD GPUs**: the ROCm package is not installed yet (CPU or Vulkan only).
- **Minimal Linux without root certificates**: Ollama's own model downloads fail (the helper has a built-in copy, Ollama doesn't). Install `ca-certificates` and restart Ollama from the admin page.
- **No code signing yet**: antivirus software may treat the binary as suspicious and delete it.

## License

Apache-2.0, see [LICENSE](LICENSE). Third-party packages (fyne.io/systray, klauspost/compress, skip2/go-qrcode, golang.org/x/sys, golang.org/x/crypto, godbus/dbus) are under Apache-2.0, MIT, or BSD licenses.
