# AGENTS.md

Go 1.26+ proxy server providing OpenAI/Claude/Codex/Grok compatible APIs with OAuth and round-robin load balancing.

## Repository
- GitHub (upstream): https://github.com/router-for-me/CLIProxyAPI
- This is a fork. `origin` is the fork, `upstream` is the original. Sync from `upstream/main`, never from `origin`.

## Provider Scope
This fork builds with three account providers only: **Codex**, **Claude**, and **xAI (Grok)**.

Removed in full: `gemini`, `gemini-interactions`, `vertex`, `aistudio`, `antigravity`, `kimi`, `meta`, `devin`.

The protocol/format layer is intentionally retained. Inbound OpenAI Chat Completions, OpenAI Responses, Claude, and Gemini-format endpoints still work and translate to codex/claude/xai, because OpenCode and Cursor consume the OpenAI-compatible format. Do not remove a provider format solely because its account provider is gone — that breaks clients, not just providers.

Two files are named after removed providers but are shared engine code, not provider code. Leave them alone:
- `internal/runtime/executor/thinking_replay_shared.go` (formerly `kimi_thinking_replay.go`) — Claude's thinking replay aliases `thinkingReplayScope` and reuses the stream accumulator and content-replay helpers.
- `internal/runtime/executor/helps/gemini_ttft_helpers.go` — parses token events at the protocol level.

## Commands
```bash
gofmt -w . # Format (required after Go changes)
go build -o cli-proxy-api ./cmd/server # Build
go run ./cmd/server # Run dev server
go test ./... # Run all tests
go test -v -run TestName ./path/to/pkg # Run single test
go build -o test-output ./cmd/server && rm test-output # Verify compile (REQUIRED after changes)
```
- Common flags: `--config <path>`, `--tui`, `--standalone`, `--local-model`, `--no-browser`, `--oauth-callback-port <port>`

## Config
- Default config: `config.yaml` (template: `config.example.yaml`)
- `.env` is auto-loaded from the working directory
- Auth material defaults under `auths/`
- Storage backends: file-based default; optional Postgres/git/object store (`PGSTORE_*`, `GITSTORE_*`, `OBJECTSTORE_*`)

## Architecture
- `cmd/server/` — Server entrypoint
- `internal/api/` — Gin HTTP API (routes, middleware, modules)
- `internal/api/modules/amp/` — Amp integration (Amp-style routes + reverse proxy)
- `internal/thinking/` — Main thinking/reasoning pipeline. `ApplyThinking()` (apply.go) parses suffixes (`suffix.go`, suffix overrides body), normalizes config to canonical `ThinkingConfig` (`types.go`), normalizes and validates centrally (`validate.go`/`convert.go`), then applies provider-specific output via `ProviderApplier`. Do not break this "canonical representation → per-provider translation" architecture.
- `internal/runtime/executor/` — Per-provider runtime executors (incl. Codex WebSocket)
- `internal/translator/` — Provider protocol translators (and shared `common`)
- `internal/registry/` — Model registry + remote updater (`StartModelsUpdater`); `--local-model` disables remote updates
- `internal/store/` — Storage implementations and secret resolution
- `internal/managementasset/` — Config snapshots and management assets
- `internal/cache/` — Request signature caching
- `internal/watcher/` — Config hot-reload and watchers
- `internal/wsrelay/` — WebSocket relay sessions
- `internal/usage/` — Usage and token accounting
- `internal/home/` — CLIProxyAPIHome control plane integration (bootstrap, RESP communication, dispatch coordination)
- `internal/tui/` — Bubbletea terminal UI (`--tui`, `--standalone`)
- `sdk/cliproxy/` — Embeddable SDK entry (service/builder/watchers/pipeline)
- `test/` — Cross-module integration tests

## Upstream Sync
Upstream ships thousands of commits and moves fast, so a merge will routinely reintroduce code this fork deleted. That is expected, not a bug in the merge.

- **Deletion conflicts resolve toward deletion.** When a merge conflict is between a deleted provider and upstream's version of it, keep the deletion. The deletions are the fork's purpose; do not restore the upstream side to "fix" the conflict.
- Never reintroduce a removed provider to resolve a compile error, a failing test, or a missing symbol. The fix is to complete the removal at the wiring site instead — the same compile-error-driven loop used for the original strip.
- Re-check the provider wiring points after every sync: `sdk/cliproxy/service_executors.go` (executor registration switch), `sdk/cliproxy/service_auth.go` and `internal/cmd/auth_manager.go` (authenticator lists), `sdk/auth/refresh_registry.go`, `internal/registry/` (model catalogs), `cmd/server/main.go` (login flags), and `internal/api/server_management.go` (management routes).
- Watch for new upstream files added under a removed provider's name. New `gemini_*`, `antigravity_*`, `kimi_*`, `meta_*`, or `devin_*` files arriving from upstream should be deleted, not kept.
- Verify with the full gate before pushing a sync: `gofmt -l .`, `go build ./...`, `go vet ./...`, `go test ./...`.

## Code Conventions
- Keep changes small and simple (KISS)
- Comments in English only
- If editing code that already contains non-English comments, translate them to English (don’t add new non-English comments)
- For user-visible strings, keep the existing language used in that file/area
- New Markdown docs should be in English unless the file is explicitly language-specific (e.g. `README_CN.md`)
- As a rule, do not make standalone changes to `internal/translator/`. You may modify it only as part of broader changes elsewhere.
- Translator-only changes are allowed in this fork without the upstream write-permission check, since this repo's own maintainers own the fork. Make the change directly and verify with the Commands gate above. The upstream permission rule and issue-first workflow apply only when contributing to `upstream`, not when working here.
- `internal/runtime/executor/` should contain executors and their unit tests only. Place any helper/supporting files under `internal/runtime/executor/helps/`.
- Payload configuration MUST be the final semantic barrier before sending requests in every executor, including streaming, WebSocket, continuation, retry/fallback, image, and token-count paths where applicable. Complete all built-in payload translation, normalization, injection, and cleanup first, then evaluate and apply user payload rules exactly once to the final business payload. No subsequent logic may overwrite configured values or restore filtered fields. Only necessary transport framing, serialization, signature calculation, and read-only validation may follow without changing business payload semantics. Add regression tests when introducing or changing request-building paths to enforce this ordering.
- Follow `gofmt`; keep imports goimports-style; wrap errors with context where helpful
- Do not use `log.Fatal`/`log.Fatalf` (terminates the process); prefer returning errors and logging via logrus
- Shadowed variables: use method suffix (`errStart := server.Start()`)
- Wrap defer errors: `defer func() { if err := f.Close(); err != nil { log.Errorf(...) } }()`
- Use logrus structured logging; avoid leaking secrets/tokens in logs
- Avoid panics in HTTP handlers; prefer logged errors and meaningful HTTP status codes
- Timeouts are allowed only during credential acquisition; after an upstream connection is established, do not set timeouts for any subsequent network behavior. Intentional exceptions that must remain allowed are the Codex websocket liveness deadlines in `internal/runtime/executor/codex_websockets_executor.go`, the wsrelay session deadlines in `internal/wsrelay/session.go`, and the management APICall timeout in `internal/api/handlers/management/api_tools.go`
- Avoid wall-clock `time.Sleep` in TTL, expiration, ordering, or cache-eviction unit tests due to platform timer granularity (e.g. Windows default timer resolution of ~15.6ms) and CI jitter under load; prefer controllable clocks (`nowFunc` / mock clock), explicit timestamp manipulation, or deterministic synchronization primitives.
- Note: if modifying features that involve CLIProxyAPIHome, check if corresponding updates are needed in the CLIProxyAPIHome repository.
- Endpoints under the `/v0/management` base URL are deprecated and no longer maintained. For any feature changes, do not modify endpoints under `/v0/management` unless necessary to fix compilation errors.
