# GoGate

A lightweight, Go-native LLM gateway. Point any OpenAI-compatible client at GoGate and route requests to **OpenAI** or **Anthropic** with virtual keys, Redis-backed budgets, and Postgres usage logging.

```
Your App (OpenAI SDK)  →  GoGate  →  OpenAI / Anthropic
                              ↓
                    Postgres (usage) + Redis (budgets)
```

## Features

- **OpenAI-compatible API** — `/v1/chat/completions`, `/v1/models`, `/v1/embeddings`
- **Anthropic Messages API** — `/v1/messages` for Claude clients
- **Virtual keys** — scoped credentials with per-key budgets and model allowlists
- **Master key admin** — bootstrap admin JWT for key management (no Dex/OIDC)
- **Budget enforcement** — async Redis checks so multiple gateway instances share state
- **Usage tracking** — every request logged to Postgres for cost reporting
- **Prometheus metrics** — `/metrics` endpoint

## Quick start

See [QUICKSTART.md](QUICKSTART.md) for a step-by-step Docker Compose setup.

```bash
cp .env.example .env   # set OPENAI_API_KEY, ANTHROPIC_API_KEY, goget_MASTER_KEY
docker compose up -d
```

## Authentication

GoGate uses two credential types:

| Credential | Purpose | Header |
|---|---|---|
| **Virtual key** (`vk_...`) | LLM API calls (`/v1/*`) | `Authorization: Bearer vk_...` |
| **Master key** | Admin bootstrap → short-lived JWT | `POST /api/admin/auth/master-key` |

1. Exchange master key for admin JWT:
   ```bash
   curl -X POST http://localhost:8080/api/admin/auth/master-key \
     -H "Content-Type: application/json" \
     -d '{"master_key":"your-master-key"}'
   ```

2. Create virtual keys with the JWT:
   ```bash
   curl -X POST http://localhost:8080/api/admin/keys \
     -H "Authorization: Bearer <jwt>" \
     -H "Content-Type: application/json" \
     -d '{"name":"app-key","max_budget":100,"budget_duration":"monthly","models":["gpt-4o-mini"]}'
   ```

3. Call LLM endpoints with the virtual key:
   ```bash
   curl -X POST http://localhost:8080/v1/chat/completions \
     -H "Authorization: Bearer vk_..." \
     -H "Content-Type: application/json" \
     -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hi"}]}'
   ```

Provider API keys stay on the server — clients never see them.

## API reference

### LLM endpoints (virtual key required)

| Method | Path | Description |
|---|---|---|
| POST | `/v1/chat/completions` | OpenAI chat completions (streaming supported) |
| POST | `/v1/messages` | Anthropic Messages API |
| POST | `/v1/completions` | Legacy completions |
| POST | `/v1/embeddings` | Embeddings |
| GET | `/v1/models` | List available models |
| GET | `/v1/models/{model}` | Get model details |

Same routes are also available under `/api/v1/*`.

### Admin endpoints (master key JWT required)

Base path: `/api/admin`

| Method | Path | Description |
|---|---|---|
| POST | `/auth/master-key` | Exchange master key for admin JWT |
| GET | `/auth/validate` | Validate current token |
| GET | `/auth/config` | Auth mode info |
| GET/POST | `/keys` | List / create virtual keys |
| GET/PUT/DELETE | `/keys/{keyID}` | Get / update / delete key |
| POST | `/keys/{keyID}/revoke` | Revoke key |
| GET | `/keys/{keyID}/stats` | Key usage stats |
| GET | `/keys/{keyID}/usage` | Key usage history |
| GET | `/stats` | Gateway stats |
| GET | `/dashboard` | Dashboard summary |
| GET | `/analytics/budget` | Budget summary |
| GET | `/analytics/usage` | Monthly usage |
| GET | `/system/health` | System health |

### Operational endpoints (no auth)

| Method | Path | Description |
|---|---|---|
| GET | `/health` | Liveness |
| GET | `/ready` | Readiness |
| GET | `/metrics` | Prometheus metrics |

## Configuration

Primary config file: `config/config.yaml`. Environment variables override values (see `.env.example`).

Key settings:

```yaml
auth:
  master_key: sk-master-dev-key-change-in-production
  require_auth: true

database:
  url: postgres://goget:goget@localhost:5432/goget?sslmode=disable

redis:
  url: redis://localhost:6379

providers:
  openai:
    enabled: true
    api_key: ${OPENAI_API_KEY}
  anthropic:
    enabled: true
    api_key: ${ANTHROPIC_API_KEY}
```

Models are defined in `config/config.yaml` under `models:` — only OpenAI and Anthropic providers are supported in this trimmed build.

## Local development

```bash
# Requires Postgres + Redis running locally
make dev          # go run ./cmd/server
make test         # go test ./...
make build        # build binary to bin/goget
```

## Architecture

```
┌─────────────┐     ┌──────────────────────────────────────┐
│   Client    │────▶│  GoGate (stateless Go HTTP server)   │
│ OpenAI SDK  │     │  ┌─────────┐  ┌────────┐  ┌───────┐ │
└─────────────┘     │  │  Auth   │→ │ Budget │→ │ LLM   │ │
                    │  │middleware│  │ Redis  │  │adapter│ │
                    │  └─────────┘  └────────┘  └───────┘ │
                    └──────────┬──────────────┬────────────┘
                               │              │
                          ┌────▼────┐    ┌────▼────┐
                          │ Postgres │    │  Redis  │
                          │ (usage)  │    │(budgets)│
                          └─────────┘    └─────────┘
```

- **Stateless gateway** — budget and rate decisions hit Redis; usage history goes to Postgres
- **Async usage pipeline** — responses are not blocked on Postgres writes
- **Provider adapters** — OpenAI and Anthropic implement a shared translation interface

## What was removed

This is a trimmed fork focused on the MVP in [prd.md](../prd.md). Removed for simplicity:

- Web UI, Swagger, Helm charts, Dex/OIDC/JWT user auth
- Guardrails (Presidio), realtime/WebSocket, files/images/audio endpoints
- Azure, Bedrock, Vertex, OpenRouter providers
- LiteLLM model-management routes (`/v1/model/*`)
- DB-backed model/route admin sync (models and routes come from `config/config.yaml` only)
- Team/user/provider/route admin CRUD

## License

MIT — see [LICENSE](LICENSE).
