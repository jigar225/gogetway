package config

import "github.com/spf13/viper"

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.metrics_port", 9090)
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "300s")
	v.SetDefault("server.idle_timeout", "120s")
	v.SetDefault("server.graceful_shutdown", "30s")

	// Database defaults
	v.SetDefault("database.max_connections", 100)
	v.SetDefault("database.max_idle_connections", 10)
	v.SetDefault("database.conn_max_lifetime", "1h")

	// Redis defaults
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 100)

	// Cache defaults
	v.SetDefault("cache.enabled", true)
	v.SetDefault("cache.ttl", "3600s")

	// Router defaults apply independently, even when routing_strategy is supplied.
	v.SetDefault("router.routing_strategy", "priority")
	v.SetDefault("router.health_check_interval", "30s")
	v.SetDefault("router.instance_retry_attempts", 2)
	v.SetDefault("router.failover_timeout_multiple", 1.5)

	// Rate limit defaults
	v.SetDefault("rate_limit.enabled", true)
	v.SetDefault("rate_limit.global_rpm", 60)

	// Monitoring defaults
	v.SetDefault("monitoring.enable_metrics", true)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.output_path", "")

	// CORS defaults
	v.SetDefault("cors.allow_credentials", true)
	v.SetDefault("cors.max_age", 86400)

	// Auth defaults
	v.SetDefault("auth.master_key", "")
	v.SetDefault("auth.jwt_secret", "")

}

func bindEnvVars(v *viper.Viper) {
	// Server
	_ = v.BindEnv("server.port", "SERVER_PORT")
	_ = v.BindEnv("server.metrics_port", "METRICS_PORT")
	_ = v.BindEnv("server.read_timeout", "SERVER_READ_TIMEOUT")
	_ = v.BindEnv("server.write_timeout", "SERVER_WRITE_TIMEOUT")
	_ = v.BindEnv("server.idle_timeout", "SERVER_IDLE_TIMEOUT")

	// Database
	_ = v.BindEnv("database.url", "DATABASE_URL")
	_ = v.BindEnv("database.max_connections", "DATABASE_MAX_CONNECTIONS")
	_ = v.BindEnv("database.max_idle_connections", "DATABASE_MAX_IDLE_CONNECTIONS")

	// Redis
	_ = v.BindEnv("redis.url", "REDIS_URL")
	_ = v.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "REDIS_DB")
	_ = v.BindEnv("redis.pool_size", "REDIS_POOL_SIZE")

	// Auth (keep the existing environment variable name for deployments)
	_ = v.BindEnv("auth.jwt_secret", "JWT_SECRET_KEY")
	_ = v.BindEnv("auth.master_key", "goget_MASTER_KEY")

	// Cache
	_ = v.BindEnv("cache.enabled", "CACHE_ENABLED")
	_ = v.BindEnv("cache.ttl", "CACHE_TTL")

	// Rate Limiting
	_ = v.BindEnv("rate_limit.global_rpm", "RATE_LIMIT_GLOBAL_RPM")

	// Monitoring
	_ = v.BindEnv("monitoring.enable_metrics", "ENABLE_METRICS")

	// Logging
	_ = v.BindEnv("logging.level", "LOG_LEVEL")
	_ = v.BindEnv("logging.format", "LOG_FORMAT")

	// CORS
	_ = v.BindEnv("cors.allowed_origins", "CORS_ALLOWED_ORIGINS")
	_ = v.BindEnv("cors.allowed_methods", "CORS_ALLOWED_METHODS")
	_ = v.BindEnv("cors.allowed_headers", "CORS_ALLOWED_HEADERS")
}
