package config

import (
	"time"
)

// ModelConfig represents a model configuration
type ModelConfig struct {
	// Required fields
	ModelName string `mapstructure:"model_name" json:"model_name"` // User-facing name (e.g., "my-gpt-4")

	Provider ProviderParams `mapstructure:"provider" json:"provider"`

	// Model information
	ModelInfo ModelInfo `mapstructure:"model_info" json:"model_info"`

	// Cost tracking (pricing overrides)
	InputCostPerToken  float64 `mapstructure:"input_cost_per_token" json:"input_cost_per_token"`
	OutputCostPerToken float64 `mapstructure:"output_cost_per_token" json:"output_cost_per_token"`

	// Optional fields
	RPM      int           `mapstructure:"rpm" json:"rpm"`           // Requests per minute
	TPM      int           `mapstructure:"tpm" json:"tpm"`           // Tokens per minute
	Timeout  time.Duration `mapstructure:"timeout" json:"timeout"`   // Request timeout
	Priority int           `mapstructure:"priority" json:"priority"` // Priority for routing
	Weight   float64       `mapstructure:"weight" json:"weight"`     // Weight for load balancing
	Tags     []string      `mapstructure:"tags" json:"tags"`         // Tags for filtering
	Enabled  *bool         `mapstructure:"enabled" json:"enabled"`   // Default true if not specified
}

// ModelInstances prepares runtime models without storing a second model list in Config.
func (c *Config) ModelInstances() []ModelInstance {
	instances := make([]ModelInstance, 0, len(c.ModelList))
	for _, model := range c.ModelList {
		instances = append(instances, ConvertToModelInstance(model))
	}
	return instances
}

// ConvertToModelInstance applies defaults to a model's runtime configuration.
func ConvertToModelInstance(cfg ModelConfig) ModelInstance {
	// Set defaults
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}

	rpm := cfg.RPM
	if rpm == 0 {
		rpm = 100 // Default RPM
	}

	tpm := cfg.TPM
	if tpm == 0 {
		tpm = 100000 // Default TPM
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	priority := cfg.Priority
	if priority == 0 {
		priority = 50 // Medium priority
	}

	weight := cfg.Weight
	if weight == 0 {
		weight = 1.0
	}

	// Use provided ModelInfo or set defaults
	modelInfo := cfg.ModelInfo
	if modelInfo.Mode == "" {
		modelInfo.Mode = "chat"
		modelInfo.SupportsStreaming = true
		modelInfo.SupportsFunctions = true
		if modelInfo.MaxTokens == 0 {
			modelInfo.MaxTokens = 128000 // Default large context
		}
		if modelInfo.DefaultMaxTokens == 0 {
			modelInfo.DefaultMaxTokens = 2000
		}
	}

	// Create ModelInstance
	return ModelInstance{
		ID:                 cfg.ModelName, // Use model_name as ID
		ModelName:          cfg.ModelName,
		Provider:           cfg.Provider,
		Source:             "system",
		ModelInfo:          modelInfo,
		InputCostPerToken:  cfg.InputCostPerToken,
		OutputCostPerToken: cfg.OutputCostPerToken,
		RPM:                rpm,
		TPM:                tpm,
		Priority:           priority,
		Weight:             weight,
		Timeout:            timeout,
		Tags:               cfg.Tags,
		Enabled:            enabled,
		MaxRetries:         3,                // Default
		CooldownPeriod:     30 * time.Second, // Default
	}
}
