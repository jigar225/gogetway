package budget

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jigar225/goget/internal/core/models"
	redisService "github.com/jigar225/goget/internal/services/data/redis"
)

// DBBudgetLoader loads budget state from Postgres for Redis cache misses.
type DBBudgetLoader struct {
	db *gorm.DB
}

// NewDBBudgetLoader creates a Postgres-backed budget loader.
func NewDBBudgetLoader(db *gorm.DB) *DBBudgetLoader {
	return &DBBudgetLoader{db: db}
}

// LoadBudget implements redis.BudgetLoader.
func (l *DBBudgetLoader) LoadBudget(ctx context.Context, entityType, entityID string) (*redisService.BudgetSnapshot, error) {
	if l.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid entity ID: %w", err)
	}

	switch entityType {
	case "key":
		var key models.Key
		if err := l.db.WithContext(ctx).First(&key, id).Error; err != nil {
			return nil, fmt.Errorf("key not found: %w", err)
		}
		if key.MaxBudget == nil || *key.MaxBudget <= 0 {
			return &redisService.BudgetSnapshot{Unlimited: true}, nil
		}
		limit := *key.MaxBudget
		spent := key.CurrentSpend
		return &redisService.BudgetSnapshot{
			Available: limit - spent,
			Spent:     spent,
			Limit:     limit,
		}, nil

	case "team":
		var team models.Team
		if err := l.db.WithContext(ctx).First(&team, id).Error; err != nil {
			return nil, fmt.Errorf("team not found: %w", err)
		}
		if team.MaxBudget <= 0 {
			return &redisService.BudgetSnapshot{Unlimited: true}, nil
		}
		return &redisService.BudgetSnapshot{
			Available: team.MaxBudget - team.CurrentSpend,
			Spent:     team.CurrentSpend,
			Limit:     team.MaxBudget,
		}, nil

	case "user":
		var user models.User
		if err := l.db.WithContext(ctx).First(&user, id).Error; err != nil {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		if user.MaxBudget <= 0 {
			return &redisService.BudgetSnapshot{Unlimited: true}, nil
		}
		return &redisService.BudgetSnapshot{
			Available: user.MaxBudget - user.CurrentSpend,
			Spent:     user.CurrentSpend,
			Limit:     user.MaxBudget,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported entity type: %s", entityType)
	}
}
