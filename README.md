# orollama — OpenRouter + Ollama proxy

A small **Go single binary** that puts **OpenRouter** and **local Ollama** behind one local API:

- **Anthropic Messages** (`/v1/messages`) for [Claude Code](https://docs.anthropic.com/en/docs/claude-code)
- **OpenAI Chat** (`/v1/chat/completions`, `/v1/models`)
- **Ollama native** (`/api/tags`, `/api/chat`, `/api/show`)

Only two upstreams. No NVIDIA, Groq, Gemini, LiteLLM, Discord bots, or free-model SQLite failover.

```mermaid
flowchart LR
  subgraph clients [Clients]
    CC[Claude_Code]
    OC[Ollama_clients]
    OA[OpenAI_clients]
  end

  subgraph proxy [orollama_on_8082]
    R[ModelRouter]
    A["/v1/messages"]
    C["/v1/chat/completions"]
    T["/api/tags /api/chat /api/show"]
  end

  subgraph upstream [Upstreams]
    OR[OpenRouter]
    OL[Local_Ollama_11434]
  end

  CC --> A
  OA --> C
  OC --> T
  A --> R
  C --> R
  T --> R
  R -->|openrouter_prefix| OR
  R -->|ollama_prefix_or_local| OL
```

---

## Ports

```mermaid
flowchart TB
  subgraph device [your_device]
    CC[Claude_Code]
    Apps[Ollama_compatible_apps]
    Proxy["orollama :8082"]
    Real["ollama serve :11434"]
  end
  OR[(OpenRouter_API)]

  CC -->|ANTHROPIC_BASE_URL| Proxy
  Apps -->|OLLAMA_HOST| Proxy
  Proxy -->|local models| Real
  Proxy -->|remote models| OR
```

| Service | Default | Role |
| --- | --- | --- |
| **orollama** | `127.0.0.1:8082` | Unified proxy |
| **Ollama** | `127.0.0.1:11434` | Real local models (`ollama run` stays here) |
| **OpenRouter** | `https://openrouter.ai/api/v1` | Remote models |

---

## Install Go (first time on macOS)

You do **not** need a system-wide Go from the installer. This project uses [mise](https://mise.jdx.dev/) (version manager) via Homebrew.

### 1. Homebrew

If you do not have Homebrew yet:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

On Apple Silicon, ensure Homebrew is on your `PATH` (add to `~/.zshrc` if needed):

```bash
eval "$(/opt/homebrew/bin/brew shellenv)"
```

### 2. Install mise

```bash
brew install mise
```

Activate mise in zsh (add to `~/.zshrc`):

```bash
eval "$(mise activate zsh)"
```

Reload:

```bash
source ~/.zshrc
mise doctor
```

### 3. Install the project’s Go version

From this repository:

```bash
cd /path/to/openrouter-ollama-proxy
mise trust
mise install
go version   # should show go1.25.x
```

`mise.toml` pins `go = "1.25"`. mise downloads the official toolchain; no global brew `go` formula is required.

---

## Build & run

```bash
cp .env.example .env
# edit .env — set OPENROUTER_API_KEY at minimum

make build    # → bin/orollama
make run      # builds then runs (colorful TTY logs)
```

Or:

```bash
go run ./cmd/orollama
```

In a real terminal you get a startup banner plus colored request lines, for example:

```text
╭────────────────────────────────────────────────────────╮
│ orollama  OpenRouter + Ollama proxy
├────────────────────────────────────────────────────────┤
│ listen   http://127.0.0.1:8082
│ ollama   http://127.0.0.1:11434
│ openrouter  https://openrouter.ai/api/v1
│ auth     on
╰────────────────────────────────────────────────────────╯
15:04:05 INFO  ready — waiting for requests
15:04:12 POST   /v1/messages 200 842ms
```

Colors auto-disable when not attached to a TTY, or when `NO_COLOR` is set. Works in macOS Terminal, iTerm, Windows Terminal, and Linux terminals (ANSI / VT mode).

Check health:

```bash
curl -s http://127.0.0.1:8082/healthz | jq .
curl -s http://127.0.0.1:8082/
# → Ollama is running
```

### CI and releases

GitHub Actions runs **build + `go test -race`** on every push/PR to `main` (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)).

Tagged releases (`v*`) cross-compile `orollama` binaries and attach them to a **GitHub Release** (linux/darwin/windows — see [`.github/workflows/release.yml`](.github/workflows/release.yml)). There is no Docker / `ghcr.io` publish; this is a single Go binary.

```bash
# From a GitHub Release asset (after you push a tag, e.g. v0.2.0)
# download orollama-darwin-arm64 (or your OS/arch), chmod +x, move to PATH

# Or install from the Go module (proxy.golang.org indexes git tags — not the GitHub Packages tab)
go install github.com/itzreqle/openrouter-ollama-proxy/cmd/orollama@latest
# pin a version:
go install github.com/itzreqle/openrouter-ollama-proxy/cmd/orollama@v0.2.0
```

Cut a release:

```bash
git tag v0.2.0
git push origin v0.2.0
```

---

## Configuration

Copy [`.env.example`](.env.example) to `.env`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `HOST` / `PORT` | `127.0.0.1` / `8082` | Listen address (non-loopback + empty `PROXY_AUTH_TOKEN` logs a loud warning) |
| `OPENROUTER_API_KEY` | — | Required for OpenRouter routes |
| `OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | OpenRouter API root |
| `OLLAMA_BASE_URL` | `http://127.0.0.1:11434` | Local Ollama |
| `PROXY_AUTH_TOKEN` | empty | If set, clients must send `Authorization: Bearer …` or `x-api-key` |
| `OROLLAMA_WRITE_CLAUDE_SETTINGS` | `1` | Write `modelPicker` into `~/.claude/settings.json` (set `0` to disable) |
| `MODEL_OPUS` / `MODEL_SONNET` / `MODEL_HAIKU` / `MODEL` | empty | Claude Code role aliases; leave empty to auto-pick from ranked catalog |
| `OPENROUTER_SORT` | `intelligence-high-to-low` | OpenRouter models list sort (website order) |
| `OPENROUTER_FREE_ONLY` | `true` | Prefer ids containing `:free` |
| `OPENROUTER_TOOLS_ONLY` | `true` | Prefer tool-capable models for auto-pick |
| `CATALOG_CACHE_SECONDS` | `600` | How often to refresh the OpenRouter ranked catalog |
| `OLLAMA_TAG_CACHE_SECONDS` | `30` | How often to refresh local model names |

---

## Model routing

```mermaid
flowchart TD
  req[Incoming_model_string]
  req --> p1{ollama_claude_or_ollama_prefix}
  p1 -->|yes| OL[Route_Ollama]
  p1 -->|no| p2{openrouter_claude_or_openrouter_or_slash}
  p2 -->|yes| OR[Route_OpenRouter]
  p2 -->|no| p3{alias_opus_sonnet_haiku}
  p3 -->|yes| mapped[Resolve_env_MODEL_star]
  mapped --> p2
  p3 -->|no| p4{in_ollama_tag_cache}
  p4 -->|yes| OL
  p4 -->|no| err[404_unknown_model]
```

| Request model | Goes to |
| --- | --- |
| `ollama-claude/qwen3-coder` | Local Ollama as `qwen3-coder` (preferred; also `ollama/…`) |
| `openrouter-claude/anthropic/claude-sonnet-4` | OpenRouter as `anthropic/claude-sonnet-4` (preferred; also `openrouter/…`) |
| `deepseek/deepseek-r1:free` | OpenRouter (any `vendor/model` id) |
| `qwen3-coder` | Ollama **if** that tag exists locally |
| `claude-3-5-sonnet-latest` | Mapped via `MODEL_SONNET` |

---

## Use with Claude Code

```mermaid
sequenceDiagram
  participant CC as Claude_Code
  participant P as orollama_8082
  participant OR as OpenRouter
  participant OL as Ollama_11434

  CC->>P: POST /v1/messages model=claude-sonnet...
  P->>P: alias + route
  alt OpenRouter model
    P->>OR: POST /messages Anthropic_skin
    OR-->>P: stream SSE
  else Ollama model
    P->>OL: POST /v1/messages
    OL-->>P: stream SSE
  end
  P-->>CC: Anthropic response
```

### Recommended: plain `claude` (auto proxy)

Works on **macOS**, **Linux**, and **Windows** (PowerShell or Git Bash / WSL).

#### macOS / Linux (zsh or bash)

```bash
cd ~/Projects/applications/openrouter-ollama-proxy
make install-cli
source ~/.zshrc    # or: source ~/.bashrc
```

#### Windows (PowerShell)

```powershell
cd $HOME\Projects\applications\openrouter-ollama-proxy
.\scripts\install-cli.ps1
. $PROFILE
```

Git Bash / WSL on Windows can use the same `make install-cli` flow as Linux.

#### Daily workflow (all platforms)

```bash
# terminal 1 — start the proxy
make run          # macOS/Linux/Git Bash
# or:  go run ./cmd/orollama

# terminal 2 — any project; just run claude
claude
```

When the proxy is **up**, the shell hook prints a colored status:

```text
orollama: proxy ON  http://127.0.0.1:8082
  provider  OpenRouter + local Ollama
  opus      openrouter-claude/minimax/minimax-m3.1-flash:free
  sonnet    openrouter-claude/nvidia/nemotron-3.5-super-vl:free
  haiku     openrouter-claude/thinkingmachines/inkling-small:free
```

Then Claude Code’s `/model` picker lists the ranked OpenRouter catalog plus local Ollama tags (via `~/.claude/settings.json` `modelPicker`).

When the proxy is **down**, `claude` runs normally (no gateway) and prints `orollama: proxy OFF`.

You can still call the helper explicitly: `claude-orollama` or `make claude`.

Optional overrides:

| Variable | Meaning |
| --- | --- |
| `OROLLAMA_ENV` | Path to `.env` (default: this repo’s `.env`) |
| `OROLLAMA_URL` | Proxy URL (default: `http://127.0.0.1:8082`) |
| `OROLLAMA_WRITE_CLAUDE_SETTINGS` | `1` — proxy updates `modelPicker` only; set `0` to leave `settings.json` alone |
| `NO_COLOR` | Disable ANSI colors in proxy logs and shell status |
| `OROLLAMA_FORCE_COLOR` | Set to `1` to force colors when stdout is not a TTY |

### Manual exports (if you prefer)

Use an **absolute** path to `.env` — a relative `.env` fails when your cwd is another project:

```bash
PROXY_ENV="$HOME/Projects/applications/openrouter-ollama-proxy/.env"

export ANTHROPIC_BASE_URL="http://127.0.0.1:8082"
export ANTHROPIC_AUTH_TOKEN="$(grep '^PROXY_AUTH_TOKEN=' "$PROXY_ENV" | cut -d= -f2-)"
export ANTHROPIC_API_KEY=""
export ANTHROPIC_DEFAULT_OPUS_MODEL="openrouter-claude/minimax/minimax-m3.1-flash:free"
export ANTHROPIC_DEFAULT_SONNET_MODEL="openrouter-claude/nvidia/nemotron-3.5-super-vl:free"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="openrouter-claude/thinkingmachines/inkling-small:free"
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1

cd ~/Projects/websites/smart   # or any project
claude
```

### Important: use `claude`, not `ollama launch claude`

| Command | Uses | OpenRouter via orollama? |
| --- | --- | --- |
| `claude` (with `make install-cli` hook) when proxy is up | Proxy on `:8082` | Yes |
| `claude-orollama` / `make claude` | Proxy on `:8082` | Yes |
| `ollama launch claude` | Ollama’s own catalog / cloud models | No |

### Verify inside Claude Code

```text
/status
```

You should see `Anthropic base URL: http://127.0.0.1:8082`. Your Sonnet pick may appear as a custom model such as `openrouter-claude/minimax/minimax-m3.1-flash:free`.

If you previously logged into Anthropic, run `claude auth logout` (or `/logout` in-session), quit, and relaunch. Dual Anthropic login + gateway token causes auth warnings.

### Dynamic catalog (default)

With `MODEL_*` left empty, the proxy fetches OpenRouter’s models sorted by **intelligence high → low** (same order as the website), optionally filtered to `:free` + tool-capable, and auto-assigns:

| Claude role | Auto pick |
| --- | --- |
| Opus | Rank #1 |
| Sonnet / default | Rank #2 |
| Haiku | Rank #3 |

Ids use the `openrouter-claude/` prefix (and `ollama-claude/` for local tags). Claude Code’s gateway discovery **only keeps** `/v1/models` entries whose id contains `claude` or `anthropic`, so plain `openrouter/…` ids never appear in `/model`. Legacy `openrouter/` and `ollama/` still route.

Resolved ids and context length are written to `~/.cache/orollama/resolved.env` (shell-quoted). Shell/PowerShell hooks re-load that file on every `claude` launch and own the role env vars:

- `ANTHROPIC_DEFAULT_OPUS_MODEL` / `SONNET` / `HAIKU` / `ANTHROPIC_MODEL`
- `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`
- `CLAUDE_CODE_GATEWAY_MODEL_DISCOVERY_TIMEOUT_MS=15000`
- `CLAUDE_CODE_MAX_CONTEXT_TOKENS=<context_length>`

Separately, the proxy writes **only** `modelPicker` into `~/.claude/settings.json` (with `replaceBuiltInOptions`) so `/model` lists the full OpenRouter + Ollama catalog—not only the 5 Anthropic role rows. It does **not** mutate the settings `env` block (shell owns those). Corrupt JSON is left untouched (with a warning); a `.bak` is kept before successful overwrites. Set `OROLLAMA_WRITE_CLAUDE_SETTINGS=0` to disable. Quit Claude fully after a catalog refresh so the new picker applies.

Pin any role by setting `MODEL_OPUS` / `MODEL_SONNET` / `MODEL_HAIKU` / `MODEL` in `.env` (overrides auto). Inspect the live catalog:

```bash
curl -s 'http://127.0.0.1:8082/v1/models?limit=1000' | jq '[.data[].id] | length'
curl -s http://127.0.0.1:8082/orollama/catalog | jq .
```

With `OPENROUTER_FREE_ONLY=true` (default) `/v1/models` is the ranked free **text** catalog (chat-capable; embeddings/rerank are excluded). `OPENROUTER_TOOLS_ONLY=true` only affects which models are auto-assigned to Opus/Sonnet/Haiku — the picker still lists the full free text set. Set `OPENROUTER_FREE_ONLY=false` for paid + free.

**Notes:**
- Free models are rate-limited (often 20 RPM; 50/day until you have bought ≥ $10 credits, then 1000/day). See [OpenRouter limits](https://openrouter.ai/docs/api/reference/limits).
- `:free` catalog changes; check [openrouter.ai/models?q=:free](https://openrouter.ai/models?q=%3Afree).
- Claude Code is only *guaranteed* with Anthropic first-party models; free non-Anthropic models are best-effort for tool use.

---

## Use with Ollama-compatible clients

```mermaid
sequenceDiagram
  participant App as Ollama_client
  participant P as orollama_8082
  participant OR as OpenRouter
  participant OL as Ollama_11434

  App->>P: GET /api/tags
  P->>OL: GET /api/tags
  P->>OR: GET /models
  P-->>App: merged list

  App->>P: POST /api/chat model=openrouter-claude/...
  P->>OR: POST /chat/completions
  OR-->>P: OpenAI SSE
  P-->>App: Ollama NDJSON

  App->>P: POST /api/chat model=ollama-claude/...
  P->>OL: POST /api/chat
  OL-->>P: Ollama NDJSON
  P-->>App: passthrough
```

Point the client at the proxy (not port 11434). `/api/tags` returns `ollama-claude/…` and `openrouter-claude/…` ids (legacy `ollama/` / `openrouter/` still route):

```bash
export OLLAMA_HOST="http://127.0.0.1:8082"

curl -s "$OLLAMA_HOST/api/tags" | jq '.models[].name' | head

curl -s "$OLLAMA_HOST/api/chat" -d '{
  "model": "ollama-claude/qwen3-coder",
  "stream": false,
  "messages": [{"role":"user","content":"Say hi in one word"}]
}' | jq .

# OpenRouter via Ollama API (requires OPENROUTER_API_KEY)
curl -s "$OLLAMA_HOST/api/chat" \
  -H "Authorization: Bearer local-dev-token" \
  -d '{
  "model": "openrouter-claude/openai/gpt-oss-120b:free",
  "stream": false,
  "messages": [{"role":"user","content":"Say hi"}]
}' | jq .
```

Keep using the real Ollama CLI against `11434`:

```bash
ollama list
ollama run qwen3-coder
```

---

## API surface

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/` | `"Ollama is running"` |
| `GET` | `/healthz` | Upstream status JSON |
| `POST` | `/v1/messages` | Anthropic Messages (passthrough) |
| `POST` | `/v1/chat/completions` | OpenAI chat (passthrough) |
| `GET` | `/v1/models` | Merged OpenAI model list (Ollama + ranked OpenRouter) |
| `GET` | `/api/tags` | Merged Ollama-format catalog (same ranking) |
| `GET` | `/orollama/catalog` | Debug: resolved roles + ranked OpenRouter entries |
| `GET` | `/orollama/limits` | Debug: OpenRouter key credits + free daily request quota (cached ~60s; auth required when `PROXY_AUTH_TOKEN` is set) |
| `POST` | `/api/show` | Ollama show or synthetic OpenRouter metadata |
| `POST` | `/api/chat` | Ollama passthrough or OpenRouter translator |

---

## Project layout

```text
cmd/orollama/          # main entrypoint
internal/config/       # env + .env loading
internal/catalog/      # ranked OpenRouter cache + auto Opus/Sonnet/Haiku
internal/model/        # routing, aliases, tag cache
internal/upstream/     # OpenRouter + Ollama HTTP clients
internal/translate/    # Ollama chat ↔ OpenAI (OpenRouter path only)
internal/handlers/     # HTTP handlers
internal/httpserver/   # mux, auth, logging
```

---

## Development

```bash
make test
make tidy
make install   # copies bin/orollama to ~/.local/bin
```

---

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| Claude Code still uses Anthropic login | `/logout`, set `ANTHROPIC_API_KEY=""`, relaunch |
| `401 unauthorized` | Send `Authorization: Bearer $PROXY_AUTH_TOKEN` or unset `PROXY_AUTH_TOKEN` |
| Empty OpenRouter models | Set `OPENROUTER_API_KEY`; check `curl …/healthz` |
| Bare model name 404 | Prefix with `ollama-claude/` or `openrouter-claude/` (legacy `ollama/` / `openrouter/` also work), or pull the model in Ollama |
| Port conflict | Change `PORT` in `.env` (leave Ollama on 11434) |
| Proxy rewrites my `~/.claude/settings.json` | Set `OROLLAMA_WRITE_CLAUDE_SETTINGS=0` (picker only; shell still owns `ANTHROPIC_*`) |
| Startup warns about open proxy | Bind `HOST=127.0.0.1` or set `PROXY_AUTH_TOKEN` |
| Corrupt `settings.json` not updated | Fix or restore `~/.claude/settings.json` / `.bak`; proxy refuses to overwrite invalid JSON |

### OpenRouter status codes (`402` / `403` / `429`)

orollama **passthroughs** the upstream status and logs a structured `openrouter rejected` line with `limit_source`, `remedy`, `retry_after`, and `provider_code` when present. Inspect live quota:

```bash
curl -s -H "Authorization: Bearer $PROXY_AUTH_TOKEN" http://127.0.0.1:8082/orollama/limits | jq .
```

| Status | Meaning | What to do |
| --- | --- | --- |
| `402` | Credits / in-flight spend budget | Check `limit_source` in the proxy log; add credits or wait for `Retry-After` ([limits docs](https://openrouter.ai/docs/api/reference/limits)) |
| `403` | Org/key **budget** *or* provider refuse | If log has `limit_source`, wait for `resets_at` / ask org admin; otherwise try another model |
| `429` | Free RPM/RPD (20/min; 50/day until ≥~$10 credits, then 1000/day) or provider capacity | Honor `Retry-After`; buy credits or use a paid model id |

Client cancels (Claude abort/retry) log as `upstream … client canceled`, not as a mysterious OpenRouter failure.

---

## Why not the other projects?

| Project | Why we skipped it |
| --- | --- |
| `free-claude-code` | Many providers, Discord bot, heavy Anthropic↔OpenAI conversion |
| `ollama-openrouter-proxy` + LiteLLM | Free-mode SQLite, failure DB, and a second gateway for Claude Code |

Today both **OpenRouter** (Anthropic Skin) and **Ollama** (`/v1/messages`) speak Anthropic natively, so this proxy mostly **passthroughs** and only translates for Ollama-native clients talking to OpenRouter.

---

## License

MIT
