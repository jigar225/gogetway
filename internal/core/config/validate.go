package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// validate checks settings without connecting to services or requiring credentials.
func (c *Config) validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 || c.Server.MetricsPort < 1 || c.Server.MetricsPort > 65535 {
		return fmt.Errorf("server ports must be between 1 and 65535")
	}
	if c.Monitoring.EnableMetrics && c.Server.Port == c.Server.MetricsPort {
		return fmt.Errorf("server.port and server.metrics_port must differ")
	}
	for name, value := range map[string]time.Duration{
		"server.read_timeout": c.Server.ReadTimeout, "server.write_timeout": c.Server.WriteTimeout,
		"server.idle_timeout": c.Server.IdleTimeout, "server.graceful_shutdown": c.Server.GracefulShutdown,
		"router.health_check_interval": c.Router.HealthCheckInterval, "cache.ttl": c.Cache.TTL,
	} {
		if value <= 0 {
			return fmt.Errorf("%s must be positive", name)
		}
	}
	if c.Database.MaxConnections < 1 || c.Database.MaxIdleConns < 0 || c.Database.MaxIdleConns > c.Database.MaxConnections {
		return fmt.Errorf("database pool requires max_connections > 0 and max_idle_connections between 0 and max_connections")
	}
	if c.Redis.DB < 0 || c.Redis.PoolSize < 1 {
		return fmt.Errorf("redis.db must be non-negative and redis.pool_size positive")
	}
	if c.RateLimit.GlobalRPM < 1 || c.RateLimit.ChatCompletionsRPM < 0 || c.RateLimit.CompletionsRPM < 0 || c.RateLimit.EmbeddingsRPM < 0 {
		return fmt.Errorf("rate_limit.global_rpm must be positive; endpoint limits must be non-negative")
	}
	if !validStrategy(c.Router.RoutingStrategy) {
		return fmt.Errorf("router.routing_strategy must be priority, least-latency, weighted-round-robin, or random")
	}
	if c.Database.ConnMaxLifetime < 0 {
		return fmt.Errorf("database.conn_max_lifetime must be non-negative")
	}
	if c.Router.InstanceRetryAttempts < 1 || c.Router.FailoverTimeoutMultiple <= 0 {
		return fmt.Errorf("router retry attempts and timeout multiple must be positive")
	}
	names := make(map[string]bool)
	for i, model := range c.ModelList {
		if model.ModelName == "" || names[model.ModelName] {
			return fmt.Errorf("model_list[%d] needs a unique model_name", i)
		}
		names[model.ModelName] = true
		if model.Provider.Type != "openai" && model.Provider.Type != "anthropic" {
			return fmt.Errorf("model_list[%d].provider.type must be openai or anthropic", i)
		}
		if model.Provider.Model == "" {
			return fmt.Errorf("model_list[%d].provider.model is required", i)
		}
		if model.Timeout < 0 || model.RPM < 0 || model.TPM < 0 || model.Weight < 0 || model.InputCostPerToken < 0 || model.OutputCostPerToken < 0 {
			return fmt.Errorf("model_list[%d] limits, timeout, weight, and prices must be non-negative", i)
		}
	}
	for i, route := range c.Routes {
		if route.Strategy != "" && !validStrategy(route.Strategy) {
			return fmt.Errorf("routes[%d].strategy is unsupported", i)
		}
	}
	return nil
}

func validStrategy(s string) bool {
	return s == "priority" || s == "least-latency" || s == "weighted-round-robin" || s == "random"
}

// ValidateDependencies is shared by the server and standalone usage worker.
func (c *Config) ValidateDependencies() error {
	for _, item := range []struct{ name, value, scheme string }{
		{"DATABASE_URL", c.Database.URL, "postgres"}, {"REDIS_URL", c.Redis.URL, "redis"},
	} {
		u, err := url.Parse(item.value)
		if err != nil || u.Host == "" || !(u.Scheme == item.scheme || (item.scheme == "postgres" && u.Scheme == "postgresql") || (item.scheme == "redis" && u.Scheme == "rediss")) {
			return fmt.Errorf("%s must contain a valid connection URL", item.name)
		}
	}
	return nil
}

// ValidateServer fails before opening connections when required settings are missing.
func (c *Config) ValidateServer() error {
	if err := c.ValidateDependencies(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Auth.MasterKey) == "" {
		return fmt.Errorf("goget_MASTER_KEY is required")
	}
	if strings.TrimSpace(c.Auth.JWTSecret) == "" {
		return fmt.Errorf("JWT_SECRET_KEY is required")
	}
	enabled := 0
	for i, model := range c.ModelList {
		if model.Enabled != nil && !*model.Enabled {
			continue
		}
		enabled++
		if model.Provider.APIKey == "" && !(model.Provider.Type == "anthropic" && model.Provider.OAuthToken != "") {
			return fmt.Errorf("model_list[%d] needs a provider API key (check its environment variable)", i)
		}
	}
	if enabled == 0 {
		return fmt.Errorf("model_list must contain at least one enabled model")
	}
	return nil
}
