package router

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jigar225/goget/internal/api/handlers"
	"github.com/jigar225/goget/internal/api/handlers/admin"
	"github.com/jigar225/goget/internal/core/auth"
	"github.com/jigar225/goget/internal/core/config"
	"github.com/jigar225/goget/internal/infrastructure/middleware"
	"github.com/jigar225/goget/internal/services/data/budget"
	"github.com/jigar225/goget/internal/services/data/cache"
	redisService "github.com/jigar225/goget/internal/services/data/redis"
	"github.com/jigar225/goget/internal/services/llm/models"
	"github.com/jigar225/goget/internal/services/monitoring/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func NewRouter(cfg *config.Config, logger *zap.Logger, modelManager *models.ModelManager, db *gorm.DB, pricingManager *config.ModelPricingManager) http.Handler {
	r := chi.NewRouter()

	// Initialize Redis client
	opt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		logger.Fatal("Failed to parse Redis URL", zap.Error(err))
	}

	// Override with explicit password and DB if provided
	if cfg.Redis.Password != "" {
		opt.Password = cfg.Redis.Password
	}
	if cfg.Redis.DB != 0 {
		opt.DB = cfg.Redis.DB
	}

	redisClient := redis.NewClient(opt)

	// Test Redis connection
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
	}

	// Initialize auth services
	masterKeyService := auth.NewMasterKeyService(&auth.MasterKeyConfig{
		DB:          db,
		MasterKey:   cfg.Auth.MasterKey,
		JWTSecret:   []byte(cfg.JWT.SecretKey),
		JWTIssuer:   "goget",
		TokenExpiry: 24 * time.Hour,
	})

	authService, err := auth.NewAuthService(&auth.AuthConfig{
		DB:               db,
		MasterKeyService: masterKeyService,
	})
	if err != nil {
		logger.Fatal("Failed to initialize auth service", zap.Error(err))
	}

	// Initialize metrics service if database and Redis are available
	var metricsService *metrics.MetricsService
	var metricsEmitter *metrics.MetricEventEmitter
	if db != nil {
		metricsConfig := &metrics.MetricsServiceConfig{
			DB:                db,
			Redis:             redisClient,
			Logger:            logger,
			WorkerCount:       4,
			BatchSize:         100,
			BatchTimeout:      5 * time.Second,
			AggregateInterval: 1 * time.Minute,
			EnableMonitoring:  false,
			MonitoringPort:    8082,
		}

		metricsService, err = metrics.NewMetricsService(metricsConfig)
		if err != nil {
			logger.Warn("Failed to initialize metrics service", zap.Error(err))
		} else {
			// Start metrics service
			if err := metricsService.Start(context.Background()); err != nil {
				logger.Warn("Failed to start metrics service", zap.Error(err))
				metricsService = nil
			} else {
				metricsEmitter = metricsService.GetEmitter()
				logger.Info("Metrics service started successfully")
			}
		}
	}

	// Initialize historical metrics collector to aggregate existing usage data
	logger.Info("Checking metrics collector prerequisites",
		zap.Bool("db_exists", db != nil),
		zap.Bool("model_manager_exists", modelManager != nil))
	if db != nil && modelManager != nil {
		historicalCollector := metrics.NewMetricsCollector(db, logger, modelManager)
		historicalCollector.Start()
		logger.Info("Historical metrics collector started")
	} else {
		logger.Warn("Cannot start historical metrics collector - missing prerequisites")
	}

	// Legacy synchronous budget/usage systems removed in favor of async Redis-based system

	// Basic middleware
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.Logger(logger))

	// Metrics middleware - use advanced metrics if available, otherwise basic
	if metricsEmitter != nil {
		r.Use(middleware.NewAsyncMetricsMiddleware(metricsEmitter, logger).Middleware)
	} else {
		r.Use(middleware.MetricsMiddleware(logger))
	}

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORS.AllowedOrigins,
		AllowedMethods:   cfg.CORS.AllowedMethods,
		AllowedHeaders:   cfg.CORS.AllowedHeaders,
		ExposedHeaders:   cfg.CORS.ExposedHeaders,
		AllowCredentials: cfg.CORS.AllowCredentials,
		MaxAge:           cfg.CORS.MaxAge,
	}))

	// Global rate limiting
	if cfg.RateLimit.Enabled {
		rateLimitMiddleware := middleware.NewRateLimitMiddleware(cfg, logger)
		r.Use(rateLimitMiddleware.Handler)
	}

	// Caching middleware
	if cfg.Cache.Enabled {
		cacheMiddleware := middleware.NewCacheMiddleware(cfg, logger)
		r.Use(cacheMiddleware.Handler)
	}

	// Health check
	r.Get("/health", handlers.Health)
	r.Get("/ready", handlers.Ready)

	// Prometheus metrics endpoint
	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	// NOTE: Swagger endpoint removed for the trimmed gateway.

	// Initialize specialized handlers
	var (
		chatHandler       *handlers.ChatHandler
		messagesHandler   *handlers.MessagesHandler
		embeddingsHandler *handlers.EmbeddingsHandler
		modelsHandler     *handlers.ModelsHandler
	)

	if metricsEmitter != nil {
		chatHandler = handlers.NewChatHandlerWithMetrics(logger, modelManager, metricsEmitter)
		messagesHandler = handlers.NewMessagesHandlerWithMetrics(logger, modelManager, metricsEmitter)
		embeddingsHandler = handlers.NewEmbeddingsHandlerWithMetrics(logger, modelManager, metricsEmitter)
		modelsHandler = handlers.NewModelsHandlerWithMetrics(logger, modelManager, pricingManager, metricsEmitter)
	} else {
		chatHandler = handlers.NewChatHandler(logger, modelManager)
		messagesHandler = handlers.NewMessagesHandler(logger, modelManager)
		embeddingsHandler = handlers.NewEmbeddingsHandler(logger, modelManager)
		modelsHandler = handlers.NewModelsHandler(logger, modelManager, pricingManager)
	}

	// Initialize system handler for auth config
	systemHandler := admin.NewSystemHandler(logger, db)

	// Public auth config (master key only)
	r.Get("/api/auth/config", systemHandler.GetAuthConfig)

	// Protected routes
	r.Group(func(r chi.Router) {
		// Authentication middleware
		authMiddleware := middleware.NewAuthMiddleware(&middleware.AuthConfig{
			Logger:           logger,
			AuthService:      authService,
			MasterKeyService: masterKeyService,
			RequireAuth:      true,
		})
		r.Use(authMiddleware.Authenticate)

		// Initialize pricing cache for better performance
		pricingCache := cache.NewPricingCache(redisClient, logger, pricingManager)

		// Load all pricing data to Redis cache on startup
		if err := pricingCache.LoadAllPricingToCache(context.Background()); err != nil {
			logger.Warn("Failed to load pricing data to cache", zap.Error(err))
		}

		// Use async budget middleware with Redis for high performance
		asyncBudgetMiddleware := middleware.NewAsyncBudgetMiddleware(&middleware.AsyncBudgetConfig{
			Logger:      logger,
			AuthService: authService,
			BudgetCache: redisService.NewBudgetCache(redisClient, logger, 5*time.Minute),
			EventPub:    redisService.NewEventPublisher(redisClient, logger),
			UsageQueue: redisService.NewUsageQueue(&redisService.UsageQueueConfig{
				Client:     redisClient,
				Logger:     logger,
				QueueName:  "usage_processing_queue",
				BatchSize:  50,
				MaxRetries: 3,
			}),
			PricingManager: pricingManager,
			PricingCache:   pricingCache,
		})
		r.Use(asyncBudgetMiddleware.EnforceBudgetAsync)

		// Async system handles both budget + usage tracking via Redis

		// OpenAI-compatible endpoints
		r.Route("/v1", func(r chi.Router) {
			// Chat completions - use a custom handler that preserves Flusher
			r.HandleFunc("/chat/completions", chatHandler.ChatCompletions)

			// Anthropic Messages API format (LiteLLM compatible)
			r.HandleFunc("/messages", messagesHandler.AnthropicMessages)

			// Completions (legacy)
			r.Post("/completions", chatHandler.Completions)

			// Embeddings
			r.Post("/embeddings", embeddingsHandler.Embeddings)

			// Models
			r.Get("/models", modelsHandler.ListModels)
			r.Get("/models/{model}", modelsHandler.GetModel)
		})
	})

	// API Key authentication routes
	r.Group(func(r chi.Router) {
		// Authentication middleware
		authMiddleware := middleware.NewAuthMiddleware(&middleware.AuthConfig{
			Logger:           logger,
			AuthService:      authService,
			MasterKeyService: masterKeyService,
			RequireAuth:      true,
		})
		r.Use(authMiddleware.Authenticate)

		// Initialize pricing cache for better performance
		pricingCache := cache.NewPricingCache(redisClient, logger, pricingManager)

		// Load all pricing data to Redis cache on startup
		if err := pricingCache.LoadAllPricingToCache(context.Background()); err != nil {
			logger.Warn("Failed to load pricing data to cache", zap.Error(err))
		}

		// Use async budget middleware with Redis for high performance
		asyncBudgetMiddleware := middleware.NewAsyncBudgetMiddleware(&middleware.AsyncBudgetConfig{
			Logger:      logger,
			AuthService: authService,
			BudgetCache: redisService.NewBudgetCache(redisClient, logger, 5*time.Minute),
			EventPub:    redisService.NewEventPublisher(redisClient, logger),
			UsageQueue: redisService.NewUsageQueue(&redisService.UsageQueueConfig{
				Client:     redisClient,
				Logger:     logger,
				QueueName:  "usage_processing_queue",
				BatchSize:  50,
				MaxRetries: 3,
			}),
			PricingManager: pricingManager,
			PricingCache:   pricingCache,
		})
		r.Use(asyncBudgetMiddleware.EnforceBudgetAsync)

		// Async system handles both budget + usage tracking via Redis

		// OpenAI-compatible endpoints with API key
		r.Route("/api/v1", func(r chi.Router) {
			// Chat completions
			r.Post("/chat/completions", chatHandler.ChatCompletions)

			// Anthropic Messages API format (LiteLLM compatible)
			r.Post("/messages", messagesHandler.AnthropicMessages)

			// Completions (legacy)
			r.Post("/completions", chatHandler.Completions)

			// Embeddings
			r.Post("/embeddings", embeddingsHandler.Embeddings)

			// Models
			r.Get("/models", modelsHandler.ListModels)
			r.Get("/models/{model}", modelsHandler.GetModel)

		})
	})

	// Admin routes - mount if database is available
	if db != nil {
		// Create unified budget service using the existing Redis components
		budgetService := budget.NewUnifiedService(&budget.UnifiedServiceConfig{
			DB:          db,
			Logger:      logger,
			BudgetCache: redisService.NewBudgetCache(redisClient, logger, 5*time.Minute),
			UsageQueue: redisService.NewUsageQueue(&redisService.UsageQueueConfig{
				Client:     redisClient,
				Logger:     logger,
				QueueName:  "usage_processing_queue",
				BatchSize:  50,
				MaxRetries: 3,
			}),
			EventPub: redisService.NewEventPublisher(redisClient, logger),
		})

		// Create admin sub-router configuration
		adminConfig := &AdminRouterConfig{
			Config:             cfg,
			Logger:             logger,
			DB:                 db,
			AuthService:        authService,
			MasterKeyService:   masterKeyService,
			ModelManager:       modelManager,
			BudgetService: budgetService,
		}

		// Mount admin routes at /api/admin
		adminSubRouter := NewAdminSubRouter(adminConfig)
		r.Mount("/api/admin", adminSubRouter)

		logger.Info("Admin routes mounted at /api/admin")
	}

	// NOTE: UI and docs routes intentionally removed for the trimmed gateway.

	// Not found handler
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if _, err := w.Write([]byte(`{"error": {"message": "Not found", "type": "invalid_request_error", "code": "not_found"}}`)); err != nil {
			logger.Error("Failed to write 404 response", zap.Error(err))
		}
	})

	return r
}
