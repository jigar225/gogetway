package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jigar225/goget/internal/core/models"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserInactive       = errors.New("user is inactive")
	ErrInvalidToken       = errors.New("invalid token")
	ErrTokenExpired       = errors.New("token expired")
	ErrInvalidAPIKey      = errors.New("invalid API key")
	ErrInvalidKey         = errors.New("invalid key")
	ErrMasterKeyRequired  = errors.New("master key required")
)

// TokenClaims is used for short-lived admin tokens issued after master-key login.
type TokenClaims struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	Groups   []string  `json:"groups"`
	Subject  string    `json:"sub"`
	Issuer   string    `json:"iss"`
	ExpiresAt int64    `json:"exp"`
}

type AuthService struct {
	db               *gorm.DB
	masterKeyService *MasterKeyService
}

type AuthConfig struct {
	DB               *gorm.DB
	MasterKeyService *MasterKeyService
}

func NewAuthService(config *AuthConfig) (*AuthService, error) {
	return &AuthService{
		db:               config.DB,
		masterKeyService: config.MasterKeyService,
	}, nil
}

// ValidateKey validates virtual/API keys against the database.
func (s *AuthService) ValidateKey(ctx context.Context, key string) (*models.Key, error) {
	keyHash := models.HashKey(key)

	var dbKey models.Key
	err := s.db.Preload("User").Preload("Team").Where("key_hash = ? AND is_active = ?", keyHash, true).First(&dbKey).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidAPIKey
		}
		return nil, err
	}

	if !dbKey.IsValid() {
		return nil, ErrInvalidAPIKey
	}

	now := time.Now()
	s.db.Model(&dbKey).Updates(map[string]interface{}{
		"last_used_at": now,
		"usage_count":  gorm.Expr("usage_count + 1"),
	})

	auditEntry := &models.Audit{
		EventType:    models.AuditEventKeyUsage,
		EventAction:  "key_validation",
		EventResult:  models.AuditResultSuccess,
		UserID:       dbKey.UserID,
		TeamID:       dbKey.TeamID,
		KeyID:        &dbKey.ID,
		AuthMethod:   "api_key",
		ResourceType: "key",
		ResourceID:   &dbKey.ID,
		Message:      "API key used successfully",
		Timestamp:    now,
	}
	s.db.Create(auditEntry)

	return &dbKey, nil
}

func (s *AuthService) ValidateAPIKey(ctx context.Context, key string) (*models.Key, error) {
	return s.ValidateKey(ctx, key)
}

func (s *AuthService) ValidateMasterKey(ctx context.Context, key string) (*MasterKeyContext, error) {
	if s.masterKeyService == nil {
		return nil, ErrMasterKeyRequired
	}
	return s.masterKeyService.ValidateMasterKey(ctx, key)
}

// CheckBudgetCached is a legacy stub kept for tests that reference AuthService directly.
func (s *AuthService) CheckBudgetCached(ctx context.Context, entityType, entityID string, estimatedCost float64) (bool, error) {
	return true, nil
}
