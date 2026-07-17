package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jigar225/goget/internal/api/handlers"
	"github.com/jigar225/goget/internal/api/handlers/admin"
	"github.com/jigar225/goget/internal/core/auth"
	"github.com/jigar225/goget/internal/core/config"
	"github.com/jigar225/goget/internal/infrastructure/middleware"
	"github.com/jigar225/goget/internal/services/data/budget"
	"github.com/jigar225/goget/internal/services/llm/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type AdminRouterConfig struct {
	Config           *config.Config
	Logger           *zap.Logger
	DB               *gorm.DB
	AuthService      *auth.AuthService
	MasterKeyService *auth.MasterKeyService
	BudgetService    budget.Service
	ModelManager     *models.ModelManager
}

// NewAdminSubRouter exposes a trimmed admin API: virtual keys, usage, and health.
func NewAdminSubRouter(cfg *AdminRouterConfig) http.Handler {
	r := chi.NewRouter()

	authHandler := admin.NewAuthHandler(cfg.Logger, cfg.MasterKeyService, cfg.AuthService, cfg.DB)
	keyHandler := admin.NewKeyHandler(cfg.Logger, cfg.DB, cfg.BudgetService)
	analyticsHandler := admin.NewAnalyticsHandler(cfg.Logger, cfg.DB, cfg.ModelManager)
	systemHandler := admin.NewSystemHandler(cfg.Logger, cfg.DB)
	dashboardHandler := handlers.NewDashboardHandler(cfg.DB, cfg.Logger)

	authMiddleware := middleware.NewAuthMiddleware(&middleware.AuthConfig{
		Logger:           cfg.Logger,
		AuthService:      cfg.AuthService,
		MasterKeyService: cfg.MasterKeyService,
		RequireAuth:      false,
	})

	// Public auth bootstrap
	r.Post("/auth/master-key", authHandler.MasterKeyLogin)
	r.Get("/auth/validate", authHandler.Validate)
	r.Get("/auth/config", systemHandler.GetAuthConfig)

	// Usage summaries (master key required for sensitive spend data)
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.Authenticate)
		r.Use(authMiddleware.RequireAdmin)

		r.Get("/stats", analyticsHandler.GetStats)
		r.Get("/dashboard", analyticsHandler.GetDashboard)
		r.Get("/dashboard/metrics", dashboardHandler.GetDashboardMetrics)
		r.Get("/dashboard/usage-trends", dashboardHandler.GetUsageTrends)

		r.Route("/keys", func(r chi.Router) {
			r.Get("/", keyHandler.ListKeys)
			r.Post("/", keyHandler.CreateKey)
			r.Post("/validate", keyHandler.ValidateKey)
			r.Get("/{keyID}", keyHandler.GetKey)
			r.Put("/{keyID}", keyHandler.UpdateKey)
			r.Delete("/{keyID}", keyHandler.DeleteKey)
			r.Post("/{keyID}/revoke", keyHandler.RevokeKey)
			r.Get("/{keyID}/stats", keyHandler.GetKeyStats)
			r.Get("/{keyID}/usage", keyHandler.GetKeyUsage)
		})

		r.Route("/analytics", func(r chi.Router) {
			r.Get("/budget", analyticsHandler.GetBudgetSummary)
			r.Get("/usage", analyticsHandler.GetUsage)
			r.Get("/usage/hourly", analyticsHandler.GetHourlyUsage)
			r.Get("/usage/daily", analyticsHandler.GetDailyUsage)
			r.Get("/usage/monthly", analyticsHandler.GetMonthlyUsage)
			r.Get("/costs", analyticsHandler.GetCosts)
		})

		r.Get("/system/health", systemHandler.GetSystemHealth)
	})

	return r
}
