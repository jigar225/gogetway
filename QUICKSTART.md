# GoGate Quick Start

Get the gateway running locally in a few minutes with Docker Compose.

## 1. Clone and configure

```bash
git clone https://github.com/jigar225/gogetway.git
cd gogetway
cp .env.example .env
```

Set provider keys and the admin master key in `.env`:

```env
GEMINI_API_KEY=your-gemini-key
JWT_SECRET_KEY=your-long-random-signing-secret
goget_MASTER_KEY=your-secure-admin-master-key
```

## 2. Start services

```bash
docker compose up -d
```

This starts the gateway, Postgres, and Redis.

## 3. Create a virtual key (admin)

Exchange the master key for a short-lived admin JWT:

```bash
curl -s -X POST http://localhost:8080/api/admin/auth/master-key \
  -H "Content-Type: application/json" \
  -d '{"master_key":"your-secure-admin-master-key"}'
```

Use the returned token to create a virtual key:

```bash
curl -s -X POST http://localhost:8080/api/admin/keys \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "dev-key",
    "max_budget": 50,
    "budget_duration": "monthly",
    "models": ["gemini-flash", "gemini-flash-lite"]
  }'
```

Save the `key` value from the response — it is only shown once.

## 4. Call the LLM API

All `/v1/*` routes require a virtual key in the `Authorization` header.

```bash
curl http://localhost:8080/health

curl http://localhost:8080/v1/models \
  -H "Authorization: Bearer vk_..."

curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer vk_..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-flash",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

Anthropic-style clients can use `/v1/messages` with the same virtual key.

### Python (OpenAI SDK)

```python
from openai import OpenAI

client = OpenAI(
    api_key="vk_your_virtual_key",
    base_url="http://localhost:8080/v1",
)

response = client.chat.completions.create(
    model="gemini-flash",
    messages=[{"role": "user", "content": "Hello!"}],
)
print(response.choices[0].message.content)
```

## 5. Monitor usage

```bash
# Gateway health
curl http://localhost:8080/health

# Prometheus metrics
curl http://localhost:8080/metrics

# Admin dashboard stats (master key JWT)
curl http://localhost:8080/api/admin/stats \
  -H "Authorization: Bearer <admin-jwt>"

# Budget summary
curl http://localhost:8080/api/admin/analytics/budget \
  -H "Authorization: Bearer <admin-jwt>"
```

## Local development (without Docker)

```bash
make dev
```

Requires Postgres and Redis. Run `docker compose up -d postgres redis`, then use the host URLs in `.env.example` (Redis is on port 6380). The same `config/config.yaml` selects models for both local Go and Compose. Real LLM requests can incur provider charges.

## Troubleshooting

**401 on `/v1/*`** — Pass a valid virtual key (`Authorization: Bearer vk_...`).

**No models listed** — Check provider API keys in `.env` and restart: `docker compose restart goget`.

**Budget exceeded** — Raise the key budget via admin API or create a new key.

**Port conflicts** — Default API port is `8080`; adjust in `docker-compose.yml`.

## Next steps

- Read [README.md](README.md) for architecture and full API reference
- Edit `config/config.yaml` for models, rate limits, and routing
- Run tests: `go test ./...`
