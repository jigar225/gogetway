# Gateway configuration

## The three inputs

1. **`config/config.yaml`**: the shared model list and intentional behavior overrides.
2. **Environment variables**: credentials, connection URLs, and deployment overrides.
3. **Go defaults**: ordinary port, timeout, and pool settings.

Priority for bound settings is environment > YAML > defaults. `.env` is just a
local way to populate the process environment, loaded by the server and worker.
It is not a second model configuration file. Existing environment variables win
when godotenv loads `.env`.

`config.Load` reads, decodes, resolves model API-key references, and validates
settings. It does not initialize pricing, open connections, or retain global config.
Startup validates required credentials and connections, initializes Postgres and
one shared Redis client, prepares models/pricing, and starts the API and worker.
Disabling response caching does not disable Redis or budgets.

## Files to understand

| File | Responsibility |
|---|---|
| `internal/core/config/config.go` | Config types and loading sequence |
| `internal/core/config/defaults.go` | Defaults and explicit environment mappings |
| `internal/core/config/validate.go` | Configuration and startup validation |
| `internal/core/config/models.go` | Model input and runtime defaults |
| `internal/core/config/model_config.go` | Runtime model, provider, and route types |
| `cmd/server/main.go` | Application startup and shutdown |

## Everyday settings

| Environment variable | YAML field | Default |
|---|---|---|
| `DATABASE_URL` | `database.url` | Required |
| `REDIS_URL` | `redis.url` | Required |
| `goget_MASTER_KEY` | `auth.master_key` | Required for server |
| `JWT_SECRET_KEY` | `auth.jwt_secret` | Required for server |
| `SERVER_PORT` | `server.port` | 8080 |
| `METRICS_PORT` | `server.metrics_port` | 9090 |
| `LOG_LEVEL` | `logging.level` | info |
| `LOG_FORMAT` | `logging.format` | json |
| `CACHE_ENABLED` | `cache.enabled` | true |
| `CACHE_TTL` | `cache.ttl` | 1h; shared YAML sets 5m |
| `RATE_LIMIT_GLOBAL_RPM` | `rate_limit.global_rpm` | 60; shared YAML sets 1000 |
| `ENABLE_METRICS` | `monitoring.enable_metrics` | true |
| `CONFIG_FILE` | Selects the YAML file itself | Standard search paths |

`GEMINI_API_KEY` is referenced by the shared model entries. Other provider secrets
can be referenced as `${VARIABLE_NAME}` in `provider.api_key`. Missing credentials
for enabled models fail server startup; disabled models do not need credentials.
Use long random admin/JWT secrets. Neither secret belongs in committed YAML.

Without `CONFIG_FILE`, the loader searches for `config.yaml` in the working
directory, `./config`, then `/etc/goget`. An explicit path can name a file or a
directory containing `config.yaml`. A missing explicit file is an error.
The standalone worker also accepts `-config` and requires Postgres and Redis.

## Optional settings

The 12 top-level sections remain because they describe working capabilities:
`server`, `database`, `redis`, `auth`, `model_list`, `router`, `routes`, `cache`,
`rate_limit`, `monitoring`, `logging`, and `cors`. Most need not appear in YAML.

- Server: `read_timeout`, `write_timeout`, `idle_timeout`, `graceful_shutdown`.
- Database: `max_connections`, `max_idle_connections`, `conn_max_lifetime`.
- Redis: `password`, `db`, `pool_size`; credentials and DB can also be in its URL.
- Rate limits: `enabled`, `global_rpm`, `chat_completions_rpm`, `completions_rpm`, `embeddings_rpm`.
- Routing: `routing_strategy`, `health_check_interval`, `enable_failover`,
  `instance_retry_attempts`, `enable_model_fallback`, `model_fallbacks`,
  `failover_timeout_multiple`.
- Supported strategies: `priority`, `least-latency`, `weighted-round-robin`, `random`.
- Named `routes` retain their model weights, priorities, and fallback lists.
- Logging supports `output_path`; it does not implement YAML log rotation settings.
- CORS retains origins, methods, headers, exposed headers, credentials, and max age.
- Provider settings: `type`, `model`, `api_key`, `base_url`, `org_id`,
  `reasoning_effort`, and Anthropic `oauth_token`.
- Model settings retain timeouts, weights, priorities, limits, metadata, tags,
  `enabled`, and explicit input/output token pricing overrides.

Metrics are served on the main API's `/metrics`. `enable_metrics` controls the
additional metrics listener on 9090. Admin endpoints use the main API port.
Admin JWTs still expire after 24 hours.

## Local, Docker, and Kubernetes

The shared YAML preserves the two Gemini model entries. Compose now uses those
same models instead of its former separate OpenAI/Anthropic list. Adapters remain
available for both OpenAI and Anthropic; configure their models to use them.

For local Go, `.env.example` uses Postgres on localhost:5432 and Redis on
localhost:6380, matching Compose's published ports. Inside Compose, the URLs use
`postgres:5432` and `redis:6379`. Kubernetes continues to supply its own URLs and
credentials through the existing ConfigMap/Secret references.

Provider calls may incur charges. Tests use dummy credentials; do not run live
LLM requests merely to check configuration. Never commit `.env` or real Secret YAML.

## Migration from the old configuration

- Delete `model_groups` and `model_aliases`; neither was used by the gateway.
- Move `jwt.secret_key` to `auth.jwt_secret`; keep `JWT_SECRET_KEY` unchanged.
- Remove JWT duration fields: admin-token duration was already hardcoded.
- Replace legacy model `params` entries with explicit `provider` entries.
- Remove `server.admin_port` and `auth.require_auth`; authentication stays required.
- Remove unused cache `max_size`/`strategy` and rate-limit `requests_per_minute`,
  `burst`, and `cleanup_interval`. Use `global_rpm` / `RATE_LIMIT_GLOBAL_RPM`.
- Use `database.max_connections` and `database.max_idle_connections` instead of
  `max_open_conns` and `max_idle_conns`.
- Remove unsupported YAML `server.host`, `database.auto_migrate`, Redis connection
  timeout knobs, `logging.file`, `auth.api_key_header`, and nested monitoring paths.
- Remove unused router `allowed_failures`, `cache_ttl`, `default_timeout`,
  `max_retries`, `enable_load_balancing`, `fallbacks`, and invented circuit-breaker
  settings. Keep the supported routing fields listed above.
- `latency-based` previously fell back to priority; the shared config now says
  `priority` explicitly. Use `least-latency` to intentionally enable that strategy.
- Remove Azure/Bedrock/Vertex provider fields: those adapters are not implemented.
- No Lite Mode: missing dependencies stop startup instead of advertising reduced
  functionality that the router could not support.
- `config/config.docker.yaml` is removed. Compose mounts `config/config.yaml`.

Unknown keys now fail decoding. This makes stale settings visible rather than
silently suggesting that they affect behavior.

## Cleanup checklist

- [x] Remove unused configuration fields and obsolete model formats.
- [x] Use one shared YAML with environment-specific URLs and secrets.
- [x] Use independent config loaders and reject unknown settings.
- [x] Validate required dependencies and credentials before startup.
- [x] Remove Lite Mode and reuse the startup Redis client.
- [x] Initialize pricing outside the loader using the actual asset path.
- [x] Update examples, Compose wiring, and migration documentation.
- [x] Verify build, configuration regressions, and focused integration checks.

Validation completed: `go build ./...`, compilation of all test packages,
configuration tests with the race detector, model tests, focused router health
and auth-configuration integration checks with local Postgres/Redis containers,
focused `go vet`, and Compose validation with dummy environment values.

The full suite is not claimed green: the earlier `Validate_Master_Key` auth test
failure was reproduced against the unchanged commit. That separate behavior was
not changed by this configuration cleanup.
