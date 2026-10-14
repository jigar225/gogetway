package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Auth     AuthConfig     `mapstructure:"auth"`

	ModelList []ModelConfig  `mapstructure:"model_list"`
	Router    RouterSettings `mapstructure:"router"`
	Routes    []RouteConfig  `mapstructure:"routes"`

	Cache      CacheConfig      `mapstructure:"cache"`
	RateLimit  RateLimitConfig  `mapstructure:"rate_limit"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
	Logging    LoggingConfig    `mapstructure:"logging"`
	CORS       CORSConfig       `mapstructure:"cors"`
}

type ServerConfig struct {
	Port             int           `mapstructure:"port"`
	MetricsPort      int           `mapstructure:"metrics_port"`
	ReadTimeout      time.Duration `mapstructure:"read_timeout"`
	WriteTimeout     time.Duration `mapstructure:"write_timeout"`
	IdleTimeout      time.Duration `mapstructure:"idle_timeout"`
	GracefulShutdown time.Duration `mapstructure:"graceful_shutdown"`
}

type DatabaseConfig struct {
	URL             string        `mapstructure:"url"`
	MaxConnections  int           `mapstructure:"max_connections"`
	MaxIdleConns    int           `mapstructure:"max_idle_connections"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

type RedisConfig struct {
	URL      string `mapstructure:"url"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

type AuthConfig struct {
	MasterKey string `mapstructure:"master_key"`
	JWTSecret string `mapstructure:"jwt_secret"`
}

type CacheConfig struct {
	Enabled bool          `mapstructure:"enabled"`
	TTL     time.Duration `mapstructure:"ttl"`
}

type RateLimitConfig struct {
	Enabled            bool `mapstructure:"enabled"`
	GlobalRPM          int  `mapstructure:"global_rpm"`
	ChatCompletionsRPM int  `mapstructure:"chat_completions_rpm"`
	CompletionsRPM     int  `mapstructure:"completions_rpm"`
	EmbeddingsRPM      int  `mapstructure:"embeddings_rpm"`
}

type MonitoringConfig struct {
	EnableMetrics bool `mapstructure:"enable_metrics"`
}

type LoggingConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	OutputPath string `mapstructure:"output_path"`
}

type CORSConfig struct {
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
	AllowedMethods   []string `mapstructure:"allowed_methods"`
	AllowedHeaders   []string `mapstructure:"allowed_headers"`
	ExposedHeaders   []string `mapstructure:"exposed_headers"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
	MaxAge           int      `mapstructure:"max_age"`
}

// Load reads one YAML file, applies environment overrides, and returns settings.
// An explicit path may name a YAML file or a directory containing config.yaml.
func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	if configPath != "" {
		if info, err := os.Stat(configPath); err == nil && info.IsDir() {
			v.SetConfigFile(filepath.Join(configPath, "config.yaml"))
		} else {
			v.SetConfigFile(configPath)
		}
	} else {
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/goget")
	}

	// Set defaults
	setDefaults(v)

	// Bind environment variables
	bindEnvVars(v)

	// Read config file if exists
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	var config Config
	if err := v.UnmarshalExact(&config); err != nil {
		return nil, fmt.Errorf("unable to decode config: %w", err)
	}

	// Resolve provider API keys after decoding the single supported model format.
	for i := range config.ModelList {
		key := config.ModelList[i].Provider.APIKey
		if strings.HasPrefix(key, "${") && strings.HasSuffix(key, "}") {
			config.ModelList[i].Provider.APIKey = os.Getenv(key[2 : len(key)-1])
		}
	}

	if err := config.validate(); err != nil {
		return nil, err
	}
	return &config, nil
}
