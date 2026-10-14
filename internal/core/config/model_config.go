package config

import (
	"time"
)

// ModelInstance represents a single model instance configuration
// Each instance can have its own API key, rate limits, and configuration
type ModelInstance struct {
	// Instance identification
	ID           string `mapstructure:"id" json:"id"`                       // Unique instance ID (auto-generated if not provided)
	ModelName    string `mapstructure:"model_name" json:"model_name"`       // User-facing model name (e.g., "gpt-4")
	InstanceName string `mapstructure:"instance_name" json:"instance_name"` // Optional instance name

	// Provider configuration
	Provider ProviderParams `mapstructure:"provider" json:"provider"`

	// Model information
	ModelInfo ModelInfo `mapstructure:"model_info" json:"model_info"`

	// Rate limiting (per deployment)
	RPM int `mapstructure:"rpm" json:"rpm"` // Requests per minute
	TPM int `mapstructure:"tpm" json:"tpm"` // Tokens per minute

	// Load balancing configuration
	Priority int     `mapstructure:"priority" json:"priority"` // Higher priority = preferred (1-100)
	Weight   float64 `mapstructure:"weight" json:"weight"`     // Weight for weighted round-robin

	// Health and retry configuration
	MaxRetries     int           `mapstructure:"max_retries" json:"max_retries"`
	Timeout        time.Duration `mapstructure:"timeout" json:"timeout"`
	CooldownPeriod time.Duration `mapstructure:"cooldown_period" json:"cooldown_period"` // After failure

	// Cost tracking
	InputCostPerToken  float64 `mapstructure:"input_cost_per_token" json:"input_cost_per_token"`
	OutputCostPerToken float64 `mapstructure:"output_cost_per_token" json:"output_cost_per_token"`

	// Custom headers
	CustomHeaders map[string]string `mapstructure:"custom_headers" json:"custom_headers"`

	// Tags for filtering and grouping
	Tags []string `mapstructure:"tags" json:"tags"`

	// Enabled flag
	Enabled bool `mapstructure:"enabled" json:"enabled"`

	// Source indicates where this model was loaded from ("system" or "user")
	Source string `mapstructure:"-" json:"source,omitempty"`
}

// ProviderParams contains provider-specific parameters
type ProviderParams struct {
	Type  string `mapstructure:"type" json:"type"`   // "openai" (including compatible APIs) or "anthropic"
	Model string `mapstructure:"model" json:"model"` // Actual model identifier (e.g., "gpt-4-turbo-preview")

	APIKey string `mapstructure:"api_key" json:"api_key"`

	BaseURL string `mapstructure:"base_url" json:"base_url"` // Base URL

	OrgID string `mapstructure:"org_id" json:"org_id"`

	ReasoningEffort string `mapstructure:"reasoning_effort" json:"reasoning_effort,omitempty"`

	OAuthToken string `mapstructure:"oauth_token" json:"oauth_token,omitempty"`
}

// ModelInfo contains model capabilities and metadata
type ModelInfo struct {
	Mode               string   `mapstructure:"mode" json:"mode"` // "chat", "completion", "embedding", "image", "audio"
	SupportsFunctions  bool     `mapstructure:"supports_functions" json:"supports_functions"`
	SupportsVision     bool     `mapstructure:"supports_vision" json:"supports_vision"`
	SupportsStreaming  bool     `mapstructure:"supports_streaming" json:"supports_streaming"`
	MaxTokens          int      `mapstructure:"max_tokens" json:"max_tokens"` // Model's context window
	MaxInputTokens     int      `mapstructure:"max_input_tokens" json:"max_input_tokens"`
	MaxOutputTokens    int      `mapstructure:"max_output_tokens" json:"max_output_tokens"`
	DefaultMaxTokens   int      `mapstructure:"default_max_tokens" json:"default_max_tokens"`
	SupportedLanguages []string `mapstructure:"supported_languages" json:"supported_languages"`
}

// RouterSettings contains load balancing and routing configuration
type RouterSettings struct {
	RoutingStrategy     string        `mapstructure:"routing_strategy" json:"routing_strategy"` // priority, least-latency, weighted-round-robin, random
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval" json:"health_check_interval"`

	// Failover configuration
	EnableFailover          bool              `mapstructure:"enable_failover" json:"enable_failover"`                     // Enable automatic failover
	InstanceRetryAttempts   int               `mapstructure:"instance_retry_attempts" json:"instance_retry_attempts"`     // Retry attempts per instance (default: 2)
	ModelFallbacks          map[string]string `mapstructure:"model_fallbacks" json:"model_fallbacks"`                     // Map of model -> fallback model
	FailoverTimeoutMultiple float64           `mapstructure:"failover_timeout_multiple" json:"failover_timeout_multiple"` // Timeout multiplier for failover attempts (default: 1.5)
	EnableModelFallback     bool              `mapstructure:"enable_model_fallback" json:"enable_model_fallback"`         // Enable fallback to different models
}

// RouteConfig represents a route defined in config.yaml
type RouteConfig struct {
	Name           string             `mapstructure:"name" json:"name"`
	Slug           string             `mapstructure:"slug" json:"slug"`
	Description    string             `mapstructure:"description" json:"description"`
	Strategy       string             `mapstructure:"strategy" json:"strategy"`
	Models         []RouteModelConfig `mapstructure:"models" json:"models"`
	FallbackModels []string           `mapstructure:"fallback_models" json:"fallback_models"`
	Enabled        *bool              `mapstructure:"enabled" json:"enabled"`
}

// RouteModelConfig represents a model entry within a route config
type RouteModelConfig struct {
	ModelName string `mapstructure:"model_name" json:"model_name"`
	Weight    int    `mapstructure:"weight" json:"weight"`
	Priority  int    `mapstructure:"priority" json:"priority"`
}
