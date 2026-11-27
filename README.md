# MIA Lead Radar

Self-hosted, multilingual Telegram buyer-intent radar. It watches the chats you choose, filters noise locally in under a millisecond, and uses your own local or BYOK LLM to qualify leads, write icebreakers, audit the radar, and coach outreach. No SaaS billing, no hardcoded proxies, no remote embedding calls on the hot path.

## Features

- **Tier-0 local filter**: NFKC normalization, script-aware tokenizer (CJK bigrams), 14 language packs, BM25 + buyer intent vs. seller-promo scoring, DIY-fatigue signals, broad-tag guard, bounded exact/Jaccard dedup. Zero network, sub-millisecond hot path.
- **14-language UI**: `en, ru, uk, es, de, fr, pt, pl, tr, it, ar, zh, ko, ja` with auto-detection, persistent switcher, and RTL Arabic.
- **Dual-mode UI**: standalone web dashboard and Telegram Mini App (HMAC `initData` auth), responsive desktop sidebar + mobile bottom navigation.
- **MTProto engine**: persistent join queue, FloodGate cooldowns, rate governor, chat quality assessment, Add My Chats, global search, captcha solving (buttons / arithmetic / optional Vision), anti-ban read-history.
- **Leads & funnel**: category/status filters, deterministic profile/message links, copy handle/ID, icebreakers in the lead's language, false-positive learning, CSV/JSON export.
- **AI Consultant**: data-driven radar audit using funnel stats, real trash samples and version history, one-click `APPLY_RADAR_SETTINGS`, roleplay + coach review.
- **Smart Noise Guard**: per-tag trash/false-positive ratio, 6h auto-throttling, manual throttle/unthrottle, funnel and daily analytics.
- **Optional Owner Bot**: owner-only `/start`, `/help`, `/status`, `/pause`, `/resume`, `/stats`, lead alerts with deep links.

## Hardware presets

| Tier | RAM | Preset | Models |
| --- | --- | --- | --- |
| Weak | < 12 GB | `weak_qwen3_1_7b`, `weak_qwen3_0_6b`, `weak_phi4_mini` | `qwen3:1.7b`, `qwen3:0.6b`, `phi4-mini` |
| Balanced (default) | 12–23 GB | `balanced_qwen35_4b` (recommended), `balanced_gemma4_e4b`, `balanced_qwen3_8b` | `qwen3.5:4b`, `gemma4:e4b`, `qwen3:8b` |
| Workstation | ≥ 24 GB | `workstation_inkling_2_mini`, `workstation_raon_1_mini`, `workstation_qwen3_14b` | `thinkingmachines/inkling-2-mini`, `raon-1-mini`, `qwen3:14b` |
| BYOK | any | OpenRouter free, OpenAI, Gemini, custom | e.g. `qwen/qwen3-8b:free`, `gpt-5-mini`, `gemini-2.5-flash` |

Local presets use Ollama at `http://localhost:11434/v1`. Cloud BYOK uses your own API key; the app never bills or proxies your traffic.

## Quick start

### Local

```bash
cp .env.example .env        # optional; edit Telegram app credentials
npm --prefix frontend install
npm --prefix frontend run build
go run ./cmd/mia
# open http://localhost:8080
```

The built SPA is embedded from `internal/server/static/dist`, so `go run ./cmd/mia` serves the dashboard directly. The Setup Wizard detects host RAM/CPU, tests the LLM endpoint, performs QR or phone MTProto login, imports your chats, and stores everything in local SQLite.

### Docker

```bash
cp .env.example .env
docker compose up --build
# dashboard: http://localhost:8080
# optional local LLM: docker compose --profile ollama up
```

Data is persisted in the `mia-data` Docker volume. The image runs as a non-root
user (UID 10001) and the SQLite database uses WAL mode. If you prefer a bind
mount, replace the volume with `./data:/app/data` and make the directory writable
by UID 10001 (`sudo chown -R 10001:10001 data`). With the `ollama` profile, set
the LLM Base URL to `http://ollama:11434/v1` in the Setup Wizard.

## Configuration

See [`.env.example`](./.env.example). Runtime settings (LLM provider/model/key, owner bot token, owner Telegram ID, noise threshold, webhook) are configured through the UI and stored in SQLite. `TELEGRAM_APP_ID` + `TELEGRAM_APP_HASH` are required for MTProto login.

Security defaults:
- Telegram Mini App `initData` is verified with HMAC-SHA256 and a 24h freshness window.
- Owner-only access via the owner Telegram ID stored in settings; bare `user_id` parameters are never trusted.
- Local browser sessions require an access code (`ACCESS_TOKEN`) or a loopback connection.
- Secrets are masked in API responses; submitting `***` keeps the stored value.
- CSRF-safe same-origin cookies, security headers, bounded API bodies, non-blocking WebSocket fan-out with per-client write deadlines.

## Development

```bash
# frontend
npm --prefix frontend install
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build

# backend
go test -count=1 ./...
go vet ./...
```

The Vite dev server proxies `/api` (including WebSocket) to `http://localhost:8080`.

## Project layout

```text
cmd/mia/                  entrypoint + graceful shutdown
internal/config/          dotenv/env config, hardware detection
internal/domain/          shared business types
internal/storage/sqlite/  SQLite WAL store, migrations, tx-aware Store
internal/filter/          Tier-0 multilingual engine
internal/llm/             OpenAI-compatible provider + presets
internal/insights/        funnel, Noise Guard, exports, webhooks
internal/telegram/        gotd/td engine, join queue, scout, captcha, pipeline
internal/bot/             optional Owner Control Bot
internal/server/          REST/WS/auth/embedded SPA
frontend/                 React 18 + TypeScript + Tailwind + Vite
```

## Licence

Private/self-hosted project. Add your preferred open-source licence before redistribution.
