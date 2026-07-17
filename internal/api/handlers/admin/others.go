package admin

import (
	"net/http"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/jigar225/goget/internal/core/config"
	"github.com/jigar225/goget/internal/core/models"
)

// AnalyticsHandler handles analytics endpoints exposed by the trimmed admin API.
type AnalyticsHandler struct {
	baseHandler
	db           *gorm.DB
	modelManager interface {
		GetModelStats() map[string]interface{}
	}
}

func NewAnalyticsHandler(logger *zap.Logger, db *gorm.DB, modelManager interface {
	GetModelStats() map[string]interface{}
}) *AnalyticsHandler {
	return &AnalyticsHandler{
		baseHandler:  baseHandler{logger: logger},
		db:           db,
		modelManager: modelManager,
	}
}

func (h *AnalyticsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	modelStats := h.modelManager.GetModelStats()

	var activeKeys int64
	h.db.Model(&models.Key{}).Where("is_active = true").Count(&activeKeys)

	stats := map[string]interface{}{
		"total_requests":   modelStats["total_requests"],
		"total_tokens":     modelStats["total_tokens"],
		"total_cost":       modelStats["total_cost"],
		"active_keys":      activeKeys,
		"load_balancer":    modelStats["load_balancer"],
		"should_shed_load": modelStats["should_shed_load"],
	}
	h.sendJSON(w, http.StatusOK, stats)
}

func (h *AnalyticsHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	modelStats := h.modelManager.GetModelStats()

	var activeKeys int64
	h.db.Model(&models.Key{}).Where("is_active = true").Count(&activeKeys)

	var topModels []struct {
		Model         string  `gorm:"column:model"`
		TotalRequests int64   `gorm:"column:total_requests"`
		TotalTokens   int64   `gorm:"column:total_tokens"`
		TotalCost     float64 `gorm:"column:total_cost"`
	}

	h.db.Raw(`
		SELECT
			model,
			COUNT(*) as total_requests,
			SUM(total_tokens) as total_tokens,
			SUM(total_cost) as total_cost
		FROM usage_logs
		WHERE created_at >= DATE_TRUNC('month', NOW())
		GROUP BY model
		ORDER BY total_cost DESC
		LIMIT 10
	`).Scan(&topModels)

	var recentActivity []struct {
		Timestamp time.Time `gorm:"column:timestamp"`
		KeyName   string    `gorm:"column:key_name"`
		Model     string    `gorm:"column:model"`
		Tokens    int64     `gorm:"column:tokens"`
	}

	h.db.Raw(`
		SELECT
			ul.created_at as timestamp,
			COALESCE(k.name, 'unknown') as key_name,
			ul.model,
			ul.total_tokens as tokens
		FROM usage_logs ul
		LEFT JOIN keys k ON ul.key_id = k.id::text
		ORDER BY ul.created_at DESC
		LIMIT 10
	`).Scan(&recentActivity)

	topModelsArray := make([]map[string]interface{}, 0, len(topModels))
	for _, model := range topModels {
		topModelsArray = append(topModelsArray, map[string]interface{}{
			"model":    model.Model,
			"requests": model.TotalRequests,
			"tokens":   model.TotalTokens,
			"cost":     model.TotalCost,
		})
	}

	recentActivityArray := make([]map[string]interface{}, 0, len(recentActivity))
	for _, activity := range recentActivity {
		recentActivityArray = append(recentActivityArray, map[string]interface{}{
			"timestamp": activity.Timestamp.Format(time.RFC3339),
			"key":       activity.KeyName,
			"action":    "API call",
			"model":     activity.Model,
			"tokens":    activity.Tokens,
		})
	}

	dashboard := map[string]interface{}{
		"stats": map[string]interface{}{
			"total_requests": modelStats["total_requests"],
			"total_tokens":   modelStats["total_tokens"],
			"total_cost":     modelStats["total_cost"],
			"active_keys":    activeKeys,
		},
		"recent_activity": recentActivityArray,
		"top_models":      topModelsArray,
	}
	h.sendJSON(w, http.StatusOK, dashboard)
}

func (h *AnalyticsHandler) GetUsage(w http.ResponseWriter, r *http.Request) {
	var dailyUsage []struct {
		Date     time.Time `gorm:"column:date"`
		Requests int64     `gorm:"column:requests"`
		Tokens   int64     `gorm:"column:tokens"`
		Cost     float64   `gorm:"column:cost"`
	}

	h.db.Raw(`
		SELECT
			DATE_TRUNC('day', created_at) as date,
			COUNT(*) as requests,
			SUM(total_tokens) as tokens,
			SUM(total_cost) as cost
		FROM usage_logs
		WHERE created_at >= DATE_TRUNC('month', NOW())
		GROUP BY DATE_TRUNC('day', created_at)
		ORDER BY date
	`).Scan(&dailyUsage)

	var totalUsage struct {
		Requests int64   `gorm:"column:requests"`
		Tokens   int64   `gorm:"column:tokens"`
		Cost     float64 `gorm:"column:cost"`
	}

	h.db.Raw(`
		SELECT
			COUNT(*) as requests,
			SUM(total_tokens) as tokens,
			SUM(total_cost) as cost
		FROM usage_logs
		WHERE created_at >= DATE_TRUNC('month', NOW())
	`).Scan(&totalUsage)

	usageArray := make([]map[string]interface{}, 0, len(dailyUsage))
	for _, day := range dailyUsage {
		usageArray = append(usageArray, map[string]interface{}{
			"date":     day.Date.Format("2006-01-02"),
			"requests": day.Requests,
			"tokens":   day.Tokens,
			"cost":     day.Cost,
		})
	}

	usage := map[string]interface{}{
		"period": time.Now().Format("2006-01"),
		"usage":  usageArray,
		"total": map[string]interface{}{
			"requests": totalUsage.Requests,
			"tokens":   totalUsage.Tokens,
			"cost":     totalUsage.Cost,
		},
	}
	h.sendJSON(w, http.StatusOK, usage)
}

func (h *AnalyticsHandler) GetHourlyUsage(w http.ResponseWriter, r *http.Request) {
	h.sendJSON(w, http.StatusOK, map[string]interface{}{"usage": []interface{}{}})
}

func (h *AnalyticsHandler) GetDailyUsage(w http.ResponseWriter, r *http.Request) {
	h.sendJSON(w, http.StatusOK, map[string]interface{}{"usage": []interface{}{}})
}

func (h *AnalyticsHandler) GetMonthlyUsage(w http.ResponseWriter, r *http.Request) {
	h.sendJSON(w, http.StatusOK, map[string]interface{}{"usage": []interface{}{}})
}

func (h *AnalyticsHandler) GetCosts(w http.ResponseWriter, r *http.Request) {
	h.sendJSON(w, http.StatusOK, map[string]interface{}{"costs": []interface{}{}})
}

func (h *AnalyticsHandler) GetBudgetSummary(w http.ResponseWriter, r *http.Request) {
	var keys []models.Key

	if err := h.db.Find(&keys).Error; err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to fetch key budget data")
		return
	}

	keyBudgets := make([]map[string]interface{}, 0)
	totalKeyBudget := 0.0
	totalKeySpent := 0.0
	keyAlerting := 0
	keyExceeded := 0

	for _, key := range keys {
		if key.MaxBudget != nil && *key.MaxBudget > 0 {
			usagePercent := (key.CurrentSpend / *key.MaxBudget) * 100

			keyBudgets = append(keyBudgets, map[string]interface{}{
				"id":            key.ID,
				"name":          key.Name,
				"type":          "key",
				"max_budget":    *key.MaxBudget,
				"current_spend": key.CurrentSpend,
				"remaining":     *key.MaxBudget - key.CurrentSpend,
				"usage_percent": usagePercent,
				"period":        key.BudgetDuration,
				"is_active":     key.IsActive,
				"reset_at":      key.BudgetResetAt,
				"total_cost":    key.TotalCost,
				"usage_count":   key.UsageCount,
			})

			totalKeyBudget += *key.MaxBudget
			totalKeySpent += key.CurrentSpend

			if usagePercent >= 80 {
				keyAlerting++
			}
			if key.CurrentSpend >= *key.MaxBudget {
				keyExceeded++
			}
		}
	}

	response := map[string]interface{}{
		"summary": map[string]interface{}{
			"total_budget":    totalKeyBudget,
			"total_spent":     totalKeySpent,
			"total_remaining": totalKeyBudget - totalKeySpent,
			"key_budget":      totalKeyBudget,
			"key_spent":       totalKeySpent,
			"alerting_count":  keyAlerting,
			"exceeded_count":  keyExceeded,
			"total_entities":  len(keyBudgets),
		},
		"key_budgets": keyBudgets,
	}

	h.sendJSON(w, http.StatusOK, response)
}

// SystemHandler handles system endpoints for the trimmed admin API.
type SystemHandler struct {
	baseHandler
	config *config.Config
	db     *gorm.DB
}

func NewSystemHandler(logger *zap.Logger, db *gorm.DB) *SystemHandler {
	return &SystemHandler{
		baseHandler: baseHandler{logger: logger},
		config:      config.Get(),
		db:          db,
	}
}

func (h *SystemHandler) GetAuthConfig(w http.ResponseWriter, r *http.Request) {
	authConfig := map[string]interface{}{
		"master_key_enabled": h.config.Auth.MasterKey != "",
		"auth_mode":          "virtual_key_and_master_key",
	}

	h.sendJSON(w, http.StatusOK, authConfig)
}

func (h *SystemHandler) GetHealth(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{
		"database": "ok",
		"cache":    "ok",
	}

	if h.db != nil {
		sqlDB, err := h.db.DB()
		if err != nil || sqlDB.PingContext(r.Context()) != nil {
			checks["database"] = "error"
		}
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"status": "healthy",
		"checks": checks,
	})
}

func (h *SystemHandler) GetSystemHealth(w http.ResponseWriter, r *http.Request) {
	h.GetHealth(w, r)
}
