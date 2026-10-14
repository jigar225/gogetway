package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jigar225/goget/internal/api/router"
	"github.com/jigar225/goget/internal/core/config"
	"github.com/jigar225/goget/internal/core/database"
	"github.com/jigar225/goget/internal/services/data/budget"
	"github.com/jigar225/goget/internal/services/data/cache"
	redisService "github.com/jigar225/goget/internal/services/data/redis"
	"github.com/jigar225/goget/internal/services/llm/models"
	"github.com/jigar225/goget/internal/services/worker"
	"github.com/jigar225/goget/pkg/logger"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// .env is a local convenience; containers receive environment variables directly.
	_ = godotenv.Load()
	cfg, err := config.Load(os.Getenv("CONFIG_FILE"))
	if err != nil {
		return err
	}
	if err := cfg.ValidateServer(); err != nil {
		return err
	}
	log, err := logger.Initialize(cfg.Logging)
	if err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}
	defer func() { _ = log.Sync() }()

	// Both dependencies are required for authenticated, budgeted requests.
	if err := database.Initialize(&database.Config{
		DSN: cfg.Database.URL, MaxConnections: cfg.Database.MaxConnections,
		MaxIdleConns: cfg.Database.MaxIdleConns, ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	}); err != nil {
		return fmt.Errorf("initialize Postgres: %w", err)
	}
	defer func() { _ = database.Close() }()
	if err := cache.Initialize(&cache.Config{
		RedisURL: cfg.Redis.URL, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	}); err != nil {
		return fmt.Errorf("initialize Redis: %w", err)
	}
	defer func() { _ = cache.Close() }()
	redisClient := cache.GetClient()
	db := database.GetDB()

	instances := cfg.ModelInstances()
	modelManager := models.NewModelManager(log, cfg.Router, redisClient)
	if err := modelManager.LoadModelInstances(instances); err != nil {
		return err
	}
	modelManager.LoadRoutes(cfg.Routes)

	pricingManager := config.GetPricingManager()
	if err := pricingManager.LoadDefaultPricing("internal/core/config"); err != nil {
		log.Warn("Default pricing unavailable; using configured model prices", zap.Error(err))
	}
	pricingManager.AddConfigOverrides(instances)

	backgroundCtx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	healthChecker := modelManager.NewHealthChecker(cfg.Router.HealthCheckInterval)
	go healthChecker.Start(backgroundCtx)

	usageProcessor := worker.NewUsageProcessor(&worker.UsageProcessorConfig{
		DB: db, Logger: log,
		UsageQueue: redisService.NewUsageQueue(&redisService.UsageQueueConfig{
			Client: redisClient, Logger: log, QueueName: "usage_processing_queue", BatchSize: 50, MaxRetries: 3,
		}),
		BudgetCache: redisService.NewBudgetCache(redisClient, log, 5*time.Minute).WithLoader(budget.NewDBBudgetLoader(db)),
		LockManager: redisService.NewLockManager(redisClient, log),
		BatchSize:   100, ProcessingInterval: 30 * time.Second,
	})
	if err := usageProcessor.Start(backgroundCtx); err != nil {
		return fmt.Errorf("start usage processor: %w", err)
	}
	defer func() { _ = usageProcessor.Stop() }()

	servers := []*http.Server{
		newServer(cfg.Server.Port, router.NewRouter(cfg, log, modelManager, db, pricingManager), cfg.Server),
	}
	if cfg.Monitoring.EnableMetrics {
		servers = append(servers, newServer(cfg.Server.MetricsPort, router.NewMetricsRouter(cfg, log), cfg.Server))
	}
	serverErrors := make(chan error, len(servers))
	for _, srv := range servers {
		go func(s *http.Server) {
			log.Info("HTTP server starting", zap.String("address", s.Addr))
			if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				serverErrors <- err
			}
		}(srv)
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	var serverErr error
	select {
	case <-quit:
	case serverErr = <-serverErrors:
	}

	// Drain HTTP requests before stopping their background usage processor.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.GracefulShutdown)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTP shutdown failed", zap.Error(err))
			_ = srv.Close()
		}
	}
	stopBackground()

	return serverErr
}

func newServer(port int, handler http.Handler, cfg config.ServerConfig) *http.Server {
	return &http.Server{
		Addr: fmt.Sprintf(":%d", port), Handler: handler,
		ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout,
	}
}
