package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAuthAndModels(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", "environment-signing-key")
	t.Setenv("GOGET_TEST_PROVIDER_KEY", "environment-provider-key")
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
auth:
  master_key: test-master
  jwt_secret: yaml-signing-key
model_list:
  - model_name: primary
    provider:
      type: openai
      model: test-model
      api_key: ${GOGET_TEST_PROVIDER_KEY}
      base_url: https://example.invalid/v1
    input_cost_per_token: 0.00001
  - model_name: disabled
    enabled: false
    timeout: 12s
    provider:
      type: anthropic
      model: test-model
`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.JWTSecret != "environment-signing-key" || c.Auth.MasterKey != "test-master" {
		t.Fatal("auth configuration or JWT environment override was lost")
	}
	models := c.ModelInstances()
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	m := models[0]
	if m.Provider.APIKey != "environment-provider-key" || m.Provider.BaseURL != "https://example.invalid/v1" {
		t.Fatal("provider settings were not preserved")
	}
	if !m.Enabled || m.Timeout != time.Minute || m.ID != "primary" || m.Source != "system" {
		t.Fatal("runtime model defaults were not preserved")
	}
	if m.InputCostPerToken != 0.00001 || models[1].Enabled || models[1].Timeout != 12*time.Second {
		t.Fatal("explicit pricing, disabled state, or timeout was lost")
	}
}

func TestShippedConfigsLoad(t *testing.T) {
	for _, name := range []string{"config.yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../../config", name))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.ModelList) == 0 {
				t.Fatal("missing models in shared configuration")
			}
			for _, model := range c.ModelInstances() {
				if model.Provider.Type == "" || model.Provider.Model == "" {
					t.Fatal("model is missing provider configuration")
				}
			}
		})
	}
}

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadIsIndependentAndRejectsUnknownKeys(t *testing.T) {
	first, err := Load(writeConfig(t, "server:\n  port: 8123\n"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(writeConfig(t, "router:\n  routing_strategy: priority\n"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Server.Port != 8123 || second.Server.Port != 8080 {
		t.Fatal("settings leaked between loads")
	}
	if second.Router.HealthCheckInterval != 30*time.Second {
		t.Fatal("partial router config lost defaults")
	}
	for _, yaml := range []string{
		"server:\n  admin_port: 8081\n", "auth:\n  require_auth: false\n",
		"database:\n  max_open_conns: 25\n", "jwt:\n  secret_key: obsolete\n",
		"model_list:\n  - model_name: old\n    params:\n      model: old\n",
	} {
		if _, err := Load(writeConfig(t, yaml)); err == nil {
			t.Fatal("obsolete setting accepted")
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("explicit missing file accepted")
	}
}

func TestEnvironmentAndServerValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost:5432/goget")
	t.Setenv("REDIS_URL", "redis://localhost:6380")
	t.Setenv("goget_MASTER_KEY", "test-master")
	t.Setenv("JWT_SECRET_KEY", "test-jwt")
	t.Setenv("GOGET_TEST_PROVIDER_KEY", "provider-key")
	t.Setenv("CACHE_ENABLED", "false")
	t.Setenv("SERVER_PORT", "8124")
	path := writeConfig(t, `
server:
  port: 8123
model_list:
  - model_name: test
    provider:
      type: openai
      model: test
      api_key: ${GOGET_TEST_PROVIDER_KEY}
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateServer(); err != nil {
		t.Fatal(err)
	}
	if c.Server.Port != 8124 || c.Cache.Enabled {
		t.Fatal("environment overrides ignored")
	}
	if c.Redis.URL == "" {
		t.Fatal("disabling response cache removed Redis settings")
	}
	t.Setenv("GOGET_TEST_PROVIDER_KEY", "")
	c, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateServer(); err == nil {
		t.Fatal("missing provider key accepted")
	}
	c.Auth.JWTSecret = ""
	if err := c.ValidateServer(); err == nil {
		t.Fatal("missing JWT secret accepted")
	}
	c.Redis.URL = ""
	if err := c.ValidateDependencies(); err == nil {
		t.Fatal("missing Redis accepted")
	}
}

func TestInvalidSettingsFailBeforeStartup(t *testing.T) {
	for _, yaml := range []string{
		"server:\n  port: 0\n", "server:\n  metrics_port: 8080\n",
		"router:\n  health_check_interval: 0s\n", "router:\n  routing_strategy: typo\n",
		"database:\n  max_connections: 5\n  max_idle_connections: 10\n",
		"redis:\n  pool_size: -1\n", "cache:\n  ttl: -1s\n",
	} {
		if _, err := Load(writeConfig(t, yaml)); err == nil {
			t.Fatalf("invalid settings accepted: %s", yaml)
		}
	}
}
