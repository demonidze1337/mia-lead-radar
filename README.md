# MIA Lead Radar

> Open-source, self-hosted AI agent that monitors Telegram chats via MTProto, filters noise locally in `<1 ms`, qualifies buyer-intent leads with local SLMs or BYOK LLMs, and serves a dual-mode **Telegram Mini App + Localhost Web Dashboard**.

*(Full-stack clean rewrite in progress — see [`MASTER_PROMPT.md`](./MASTER_PROMPT.md) and [`REFERENCE_SPEC.md`](./REFERENCE_SPEC.md) for architecture & specification.)*

## Planned Architecture

- **Backend (`Go 1.27+`)**: Modular `gotd/td` MTProto engine, token-bucket rate governor, FloodWait circuit breaker, zero-latency local prefilter, SQLite (WAL) + optional PostgreSQL, HTTP + WebSocket live terminal, `go:embed` SPA serving.
- **AI Layer (Zero Mandatory Billing)**:
  - **Tier 0 (`<1 ms`, in-process):** rule/noise/bot/HR/marketplace filter + morphological stemmer + BM25 & buyer-intent heuristics (replaces slow remote embedding calls).
  - **Tier 1 / Tier 2 (Pluggable OpenAI-compatible API):** local **Ollama / llama.cpp** (`qwen3.5:4b`, `qwen3:8b`, `gemma4`, `phi4-mini`) OR **BYOK** (OpenRouter `:free` / `thinkingmachines/inkling-2-mini` / `raon-1-mini`, OpenAI, Gemini).
- **Frontend (`React 18 + TypeScript + Tailwind + Framer Motion`)**:
  - Works both on **`http://localhost:8080`** (built-in Setup Wizard for Bot Token, MTProto login, LLM provider/key) and inside **Telegram Mini App** (with HMAC `initData` verification).
  - Desktop/Web/Mobile-safe profile & message navigation, Live AI Terminal, Radar Builder + AI Phrase Suggester, Icebreaker generator, Radar Audit & Roleplay Training.
