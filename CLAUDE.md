# CC-Connect Development Guide

## Project Overview

CC-Connect is a bridge that connects AI coding agents (Claude Code, Codex, Gemini CLI, Cursor, etc.) with messaging platforms (Feishu/Lark, Telegram, Discord, Slack, DingTalk, WeChat Work, QQ, LINE). Users interact with their coding agent through their preferred messaging app.

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   cmd/cc-connect                │  ← entry point, CLI, daemon
├─────────────────────────────────────────────────┤
│                     config/                     │  ← TOML config parsing
├─────────────────────────────────────────────────┤
│                      core/                      │  ← engine, interfaces, i18n,
│                                                 │     cards, sessions, registry
├──────────────────────┬──────────────────────────┤
│     agent/           │      platform/           │
│  ├── claudecode/     │  ├── feishu/             │
│  ├── codex/          │  ├── telegram/           │
│  ├── cursor/         │  ├── discord/            │
│  ├── gemini/         │  ├── slack/              │
│  ├── iflow/          │  ├── dingtalk/           │
│  ├── opencode/       │  ├── wecom/              │
│  ├── acp/            │  ├── qq/                 │
│  └── qoder/          │  ├── qqbot/              │
│                      │  ├── line/               │
│                      │  └── weibo/              │
├──────────────────────┴──────────────────────────┤
│                     daemon/                     │  ← systemd/launchd service
└─────────────────────────────────────────────────┘
```

### Key Design Principles

**`core/` is the nucleus.** It defines all interfaces (`Platform`, `Agent`, `AgentSession`, etc.) and contains the `Engine` that orchestrates message flow. The core package must **never** import from `agent/` or `platform/`.

**Plugin architecture via registries.** Agents and platforms register themselves through `core.RegisterAgent()` and `core.RegisterPlatform()` in their `init()` functions. The engine creates instances via `core.CreateAgent()` / `core.CreatePlatform()` using string names from config.

**Dependency direction:**
```
cmd/ → config/, core/, agent/*, platform/*
agent/*   → core/   (never other agents or platforms)
platform/* → core/  (never other platforms or agents)
core/     → stdlib only (never agent/ or platform/)
```

### Core Interfaces

- **`Platform`** — messaging platform adapter (Start, Reply, Send, Stop)
- **`Agent`** — AI coding agent adapter (StartSession, ListSessions, Stop)
- **`AgentSession`** — a running bidirectional session (Send, RespondPermission, Events)
- **`Engine`** — the central orchestrator that routes messages between platforms and agents

Optional capability interfaces (implement only when needed):
- `CardSender` — rich card messages
- `InlineButtonSender` — inline keyboard buttons
- `ProviderSwitcher` — multi-model switching
- `DoctorChecker` — agent-specific health checks
- `AgentDoctorInfo` — CLI binary metadata for diagnostics

## Development Rules

### 1. No Hardcoding Platform or Agent Names in Core

The `core/` package must remain agnostic. Never write `if p.Name() == "feishu"` or `CreateAgent("claudecode", ...)` in core. Use interfaces and capability checks instead:

```go
// BAD — hardcodes platform knowledge in core
if p.Name() == "feishu" && supportsCards(p) {

// GOOD — capability-based check
if supportsCards(p) {
```

```go
// BAD — hardcodes agent type
agent, _ := CreateAgent("claudecode", opts)

// GOOD — derives from current agent
agent, _ := CreateAgent(e.agent.Name(), opts)
```

### 2. Prefer Interfaces Over Type Switches

When behavior differs across platforms/agents, define an optional interface in core and let implementations opt in:

```go
// In core/
type AgentDoctorInfo interface {
    CLIBinaryName() string
    CLIDisplayName() string
}

// In agent/claudecode/
func (a *Agent) CLIBinaryName() string  { return "claude" }
func (a *Agent) CLIDisplayName() string { return "Claude" }

// In core/ — query via interface, fallback gracefully
if info, ok := agent.(AgentDoctorInfo); ok {
    bin = info.CLIBinaryName()
}
```

### 3. Configuration Over Code

- Features that may vary per deployment should be configurable in `config.toml`
- Use `map[string]any` options for agent/platform factories to stay flexible
- Add new config fields with sensible defaults so existing configs don't break

### 4. High Cohesion, Low Coupling

- Each `agent/X/` package is self-contained: it handles process lifecycle, output parsing, and session management for agent X
- Each `platform/X/` package is self-contained: it handles API connection, message receiving/sending, and card rendering for platform X
- Cross-cutting concerns (i18n, cards, streaming, rate limiting) live in `core/`

### 5. Error Handling

- Always wrap errors with context: `fmt.Errorf("feishu: reply card: %w", err)`
- Never silently swallow errors; at minimum log them with `slog.Error` / `slog.Warn`
- Use `slog` (structured logging) consistently; never `log.Printf` or `fmt.Printf` for runtime logs
- Redact tokens/secrets in error messages using `core.RedactToken()`

### 6. Concurrency Safety

- Agent sessions are accessed from multiple goroutines; protect shared state with `sync.Mutex` or `atomic` types
- Use `context.Context` for cancellation propagation
- Channels should have clear ownership; document who closes them
- Prefer `sync.Once` for one-time teardown (`pendingPermission.resolve()`)

### 7. i18n

All user-facing strings must go through `core/i18n.go`:
- Define a `MsgKey` constant
- Add translations for all supported languages (EN, ZH, ZH-TW, JA, ES)
- Use `e.i18n.T(MsgKey)` or `e.i18n.Tf(MsgKey, args...)`

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `strings.EqualFold` for case-insensitive comparisons
- Avoid `init()` for anything other than platform/agent registration
- Keep functions focused; extract helpers when a function exceeds ~80 lines
- Naming: `New()` for constructors, `Get/Set` for accessors, avoid stuttering (`feishu.FeishuPlatform` → `feishu.Platform`)

## Testing

### Requirements

- All new features must include unit tests
- All bug fixes should include a regression test
- Tests must pass before committing: `go test ./...`

### Running Tests

```bash
# Full test suite
go test ./...

# Specific package
go test ./core/ -v

# Run specific test
go test ./core/ -run TestHandlePendingPermission -v

# With race detector (CI)
go test -race ./...
```

### Test Patterns

- Use stub types for `Platform` and `Agent` in core tests (see `core/engine_test.go`)
- Test card rendering by inspecting the returned `*Card` struct, not JSON
- For agent session tests, simulate event streams via channels

## Selective Compilation

Each agent and platform is imported via a separate `plugin_*.go` file with a
build tag (e.g. `//go:build !no_feishu`). By default **all** agents and
platforms are compiled in.

### Include only specific agents/platforms

```bash
# Only Claude Code agent + Feishu and Telegram platforms
make build AGENTS=claudecode PLATFORMS_INCLUDE=feishu,telegram

# Multiple agents
make build AGENTS=claudecode,codex PLATFORMS_INCLUDE=feishu,telegram,discord
```

### Exclude specific agents/platforms

```bash
# Exclude some platforms you don't need
make build EXCLUDE=discord,dingtalk,qq,qqbot,line
```

### Direct build tag usage (without Make)

```bash
go build -tags 'no_discord no_dingtalk no_qq no_qqbot no_line' ./cmd/cc-connect
```

Available tags: `no_acp`, `no_claudecode`, `no_codex`, `no_cursor`, `no_gemini`,
`no_iflow`, `no_opencode`, `no_qoder`, `no_feishu`, `no_telegram`,
`no_discord`, `no_slack`, `no_dingtalk`, `no_wecom`, `no_weixin`, `no_qq`, `no_qqbot`,
`no_line`, `no_weibo`.

## Pre-Commit Checklist

1. **Build passes**: `go build ./...`
2. **Tests pass**: `go test ./...`
3. **No new hardcoded platform/agent names in core**: grep for platform names in `core/*.go`
4. **i18n complete**: all new user-facing strings have translations for all languages
5. **No secrets in code**: no API keys, tokens, or credentials in source files

## Adding a New Platform

1. Create `platform/newplatform/newplatform.go`
2. Implement `core.Platform` interface (and optional interfaces as needed)
3. Register in `init()`: `core.RegisterPlatform("newplatform", factory)`
4. Create `cmd/cc-connect/plugin_platform_newplatform.go` with `//go:build !no_newplatform` tag
5. Add `newplatform` to `ALL_PLATFORMS` in `Makefile`
6. Add config example in `config.example.toml`
7. Add unit tests

## Adding a New Agent

1. Create `agent/newagent/newagent.go`
2. Implement `core.Agent` and `core.AgentSession` interfaces
3. Register in `init()`: `core.RegisterAgent("newagent", factory)`
4. Create `cmd/cc-connect/plugin_agent_newagent.go` with `//go:build !no_newagent` tag
5. Add `newagent` to `ALL_AGENTS` in `Makefile`
6. Optionally implement `AgentDoctorInfo` for `cc-connect doctor` support
7. Add config example in `config.example.toml`
8. Add unit tests

---

## Branch-Specific Feature: Statusline Footer

> This branch (`cc-connect-statusline-build`) carries a feature on top of
> upstream `cc-connect`: an optional **Claude-style quota/model footer**
> appended to assistant replies. When editing this branch, keep the docs below
> in sync with the implementation.

### What it does

Before delivering an assistant reply, the engine can fetch the user's quota
from an external endpoint and render a footer (model name + USD usage +
percent + reset-remaining). On platforms that support structured replies (e.g.
Feishu), it renders as native interactive tags/cards; everywhere else it falls
back to a flattened text suffix.

### Configuration

Add a `[statusline_footer]` block to `config.toml` (global), optionally
overridden per project under `[projects.<name>.statusline_footer]`:

```toml
[statusline_footer]
enabled    = true        # default false
url        = "https://example.com/api/quota"
token      = ""          # bearer / x-api-key value (or use token_env)
token_env  = "QUOTA_TOKEN" # env var fallback when token is empty
timeout_ms = 1500        # HTTP timeout, default 1500
cache_secs = 30          # cache TTL, default 30
```

Project-level values are layered over the global block by
`config.EffectiveStatuslineFooter(cfg, proj)` (only non-zero fields override).

### Code map

| Concern | Location |
| ------- | -------- |
| Config structs + merge | `config/config.go` (`StatuslineFooterConfig`, `EffectiveStatuslineFooter`) |
| Wiring config → engine | `cmd/cc-connect/main.go` (`toCoreStatuslineFooterConfig` → `engine.SetStatuslineFooterConfig`) |
| Engine cfg + fetch/cache | `core/engine.go` (`StatuslineFooterCfg`, `statuslineFooterData`, `fetchStatuslineQuota`, `formatStatuslineFooterText`) |
| Structured render interfaces | `core/interfaces.go` (`StatuslineFooterData`, `StatuslineReplySender`, `StatuslineReplyUpdater`) |
| In-place preview finalize | `core/streaming.go` (`finishStatusline`) |
| Feishu native rendering | `platform/feishu/feishu.go` (`SendStatuslineReply`, `UpdateStatuslineReply`, `buildStructuredStatuslineCardJSON`, `splitStatuslineFooter`) |

### Quota endpoint contract

`GET <url>` with both `x-api-key: <token>` and `Authorization: Bearer <token>`
headers (token resolved from `token`, else `token_env`). Expected JSON:

```json
{ "success": true,
  "data": { "usedUsd": 1.23, "limitUsd": 20.0, "percent": 6, "resetAt": "2026-06-03T00:00:00Z" } }
```

Failures are non-fatal: the engine logs at `slog.Debug` and falls back to the
last cached footer (or omits it). Keep this behavior — the footer must never
block or fail a reply.

---

## Building via GitHub Workflows (CI/CD)

All compilation is wired through GitHub Actions in `.github/workflows/`. There
are two distinct Go pipelines — know which one you are touching:

| Workflow | File | Trigger | Purpose |
| -------- | ---- | ------- | ------- |
| **CI** | `ci.yml` | push/PR to `main`, `release` published | Lint + full test matrix (the gate for upstream code) |
| **Statusline artifact** | `build-statusline-artifact.yml` | push to `cc-connect-statusline-build`, `workflow_dispatch` | Cross-compile the Windows binary with the statusline feature |
| Issue auto-reply | `issue-reply.yml` | issue events | Non-build automation |
| Stale bot | `stale.yml` | schedule | Non-build automation |

### Common build setup

Every Go job uses the same toolchain bootstrap, so reproduce it locally the
same way:

- **Go version is pinned by `go.mod`** (currently `go 1.25.0`) via
  `actions/setup-go@v5` with `go-version-file: go.mod` + module cache. Do not
  hardcode a Go version in the workflow; bump `go.mod` instead.
- The web UI (`web/`) is a pnpm project; `ci.yml` builds it (`pnpm install
  --frozen-lockfile && pnpm build`) before Go steps because Go embeds the
  built assets. The statusline artifact skips this by building with the
  `no_web` tag (see below).

### `build-statusline-artifact.yml` — the statusline Windows build

This is the workflow to use/extend when the deliverable is the patched
binary. It runs on every push to `cc-connect-statusline-build` (and can be run
manually from the Actions tab via **Run workflow** / `workflow_dispatch`).
Steps:

1. **Checkout** + **setup-go** (from `go.mod`, cached).
2. **Format & test only the touched packages** — `gofmt -w` the statusline
   files, then `go test ./config ./core ./platform/feishu -run '<statusline
   tests>'`. This is a fast gate, *not* the full suite. If you add statusline
   tests, add their names to the `-run` regex so CI actually exercises them.
3. **Build the Windows executable**:
   ```bash
   GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
     go build -tags no_web \
     -ldflags "-s -w -X main.version=v1.3.3-beta.4-statusline \
               -X main.commit=${GITHUB_SHA::7} \
               -X main.buildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
     -o dist/cc-connect.exe ./cmd/cc-connect
   sha256sum dist/cc-connect.exe > dist/checksums.txt
   ```
   - `-tags no_web` drops the embedded web UI so no pnpm/Node build is needed.
   - `CGO_ENABLED=0` produces a static, cross-compiled binary.
   - Version metadata is injected via `-ldflags -X main.version/commit/buildTime`.
4. **Upload artifact** `cc-connect-statusline-windows-amd64` containing
   `dist/cc-connect.exe` + `dist/checksums.txt`. Download it from the run's
   **Artifacts** section on the Actions page.

**Reproduce the artifact build locally** (identical to CI):

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -tags no_web -ldflags "-s -w" -o dist/cc-connect.exe ./cmd/cc-connect
```

To build other OS/arch targets, change `GOOS`/`GOARCH` (see `PLATFORMS` in the
`Makefile`: linux/darwin/windows × amd64/arm64), or use `make build` /
`make release`.

### `ci.yml` — the correctness gate (runs on `main`)

Sequential jobs (each `needs` the previous), so a failure short-circuits the
rest:

1. **lint** — builds web assets, runs `golangci-lint v2.11.4` (incrementally
   via `--new-from-rev` against the base/previous commit so only *new* issues
   fail), and `actionlint` on the workflow files.
2. **unit-test** — `go mod download` + `go mod verify` (catches `go.sum`
   drift), `go build ./...`, `go test ./... -race`, then coverage → Codecov.
3. **smoke-test** — `go test -tags=smoke,no_web ./tests/e2e/...`.
4. **regression-test** — `go test -tags=regression,no_web ./tests/e2e/...`.
5. **performance-test** — `go test -bench=. -benchmem -tags=performance,no_web
   ./tests/performance/...`.

> Note: `ci.yml` is scoped to `main`. Pushes to
> `cc-connect-statusline-build` only trigger the artifact build, **not** the
> full CI gate — so run `go build ./...` and `go test ./...` locally before
> pushing statusline changes.

### Build-related conventions

- **Never commit secrets** to workflows; pass tokens via repo
  **Settings → Secrets and variables → Actions** and reference them as
  `${{ secrets.NAME }}`.
- Workflows are linted by `actionlint` in CI — keep YAML valid and quoted.
- When adding a new test that the statusline artifact build should guard, add
  it to the `-run` regex in `build-statusline-artifact.yml`; when adding broad
  tests, they are picked up automatically by `ci.yml`'s `go test ./...`.
