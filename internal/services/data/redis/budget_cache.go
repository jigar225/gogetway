package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var (
	// ErrBudgetUnavailable is returned when Redis/DB budget state cannot be determined.
	ErrBudgetUnavailable = errors.New("budget status unavailable")
	// ErrBudgetLoaderMissing is returned on cache miss when no DB loader is configured.
	ErrBudgetLoaderMissing = errors.New("budget cache miss and no loader configured")
)

// BudgetSnapshot is the source-of-truth budget state loaded from Postgres (or another store).
type BudgetSnapshot struct {
	Available float64
	Spent     float64
	Limit     float64
	Unlimited bool // no budget configured — always allow
}

// BudgetLoader loads budget state when Redis misses.
type BudgetLoader interface {
	LoadBudget(ctx context.Context, entityType, entityID string) (*BudgetSnapshot, error)
}

// BudgetStatus represents cached budget status
type BudgetStatus struct {
	EntityID    string    `json:"entity_id"`
	EntityType  string    `json:"entity_type"`
	Available   float64   `json:"available"`
	Spent       float64   `json:"spent"`
	Limit       float64   `json:"limit"`
	Percentage  float64   `json:"percentage"`
	IsExceeded  bool      `json:"is_exceeded"`
	Unlimited   bool      `json:"unlimited,omitempty"`
	LastUpdated time.Time `json:"last_updated"`
	TTL         int64     `json:"ttl"`
}

// Atomic hot-path scripts operate on float keys so concurrent pods never race via WATCH.
// KEYS: available, spent, unlimited
// ARGV: cost, ttl_seconds
// Returns: 1 allow, 0 deny, -1 miss
var reserveBudgetScript = redis.NewScript(`
local unlimited = redis.call('GET', KEYS[3])
if unlimited == '1' then
  return 1
end
local avail = redis.call('GET', KEYS[1])
if not avail then
  return -1
end
local available = tonumber(avail)
local cost = tonumber(ARGV[1])
if available < cost then
  return 0
end
local ttl = tonumber(ARGV[2])
local newAvail = available - cost
redis.call('SET', KEYS[1], tostring(newAvail), 'EX', ttl)
redis.call('INCRBYFLOAT', KEYS[2], cost)
redis.call('EXPIRE', KEYS[2], ttl)
return 1
`)

// KEYS: available, spent, unlimited
// ARGV: delta, ttl_seconds
var adjustSpendScript = redis.NewScript(`
local unlimited = redis.call('GET', KEYS[3])
local ttl = tonumber(ARGV[2])
local delta = tonumber(ARGV[1])
if unlimited == '1' then
  redis.call('INCRBYFLOAT', KEYS[2], delta)
  redis.call('EXPIRE', KEYS[2], ttl)
  return 1
end
local avail = redis.call('GET', KEYS[1])
if not avail then
  return -1
end
redis.call('INCRBYFLOAT', KEYS[1], -delta)
redis.call('EXPIRE', KEYS[1], ttl)
redis.call('INCRBYFLOAT', KEYS[2], delta)
redis.call('EXPIRE', KEYS[2], ttl)
return 1
`)

// BudgetCache manages distributed budget caching with Redis
type BudgetCache struct {
	client *redis.Client
	logger *zap.Logger
	ttl    time.Duration
	loader BudgetLoader
}

// NewBudgetCache creates a new budget cache
func NewBudgetCache(client *redis.Client, logger *zap.Logger, ttl time.Duration) *BudgetCache {
	if ttl == 0 {
		ttl = 5 * time.Minute
	}

	return &BudgetCache{
		client: client,
		logger: logger,
		ttl:    ttl,
	}
}

// WithLoader attaches a source-of-truth loader used on cache miss. Returns the same cache for chaining.
func (bc *BudgetCache) WithLoader(loader BudgetLoader) *BudgetCache {
	bc.loader = loader
	return bc
}

// CheckBudgetAvailable performs a budget check using Redis, loading from the DB on miss.
// This is read-only (does not reserve). Use TryReserveBudget on the LLM request hot path.
func (bc *BudgetCache) CheckBudgetAvailable(ctx context.Context, entityType, entityID string, requestCost float64) (bool, error) {
	status, err := bc.ensureBudgetStatus(ctx, entityType, entityID)
	if err != nil {
		return false, err
	}

	if status.Unlimited {
		return true, nil
	}

	return status.Available-requestCost >= 0 && !status.IsExceeded, nil
}

// TryReserveBudget atomically reserves requestCost from Available across gateway replicas.
// On cache miss it loads from the DB first. Failures return an error (fail closed).
func (bc *BudgetCache) TryReserveBudget(ctx context.Context, entityType, entityID string, requestCost float64) (bool, error) {
	if requestCost < 0 {
		return false, fmt.Errorf("request cost must be non-negative")
	}

	if _, err := bc.ensureBudgetStatus(ctx, entityType, entityID); err != nil {
		return false, err
	}

	ttlSec := int(bc.ttl.Seconds())
	if ttlSec <= 0 {
		ttlSec = 300
	}

	result, err := reserveBudgetScript.Run(ctx, bc.client, []string{
		bc.availableKey(entityType, entityID),
		bc.spentKey(entityType, entityID),
		bc.unlimitedKey(entityType, entityID),
	}, requestCost, ttlSec).Int()
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrBudgetUnavailable, err)
	}

	switch result {
	case 1:
		bc.syncStatusJSON(ctx, entityType, entityID)
		return true, nil
	case 0:
		return false, nil
	case -1:
		// Rare race: keys expired between ensure and script — reload once
		if _, err := bc.ensureBudgetStatus(ctx, entityType, entityID); err != nil {
			return false, err
		}
		result, err = reserveBudgetScript.Run(ctx, bc.client, []string{
			bc.availableKey(entityType, entityID),
			bc.spentKey(entityType, entityID),
			bc.unlimitedKey(entityType, entityID),
		}, requestCost, ttlSec).Int()
		if err != nil {
			return false, fmt.Errorf("%w: %v", ErrBudgetUnavailable, err)
		}
		if result == 1 {
			bc.syncStatusJSON(ctx, entityType, entityID)
			return true, nil
		}
		return result == 1, nil
	default:
		return false, fmt.Errorf("%w: unexpected reserve result %d", ErrBudgetUnavailable, result)
	}
}

// RefundSpend reverses a prior reservation (e.g. when the upstream request fails).
func (bc *BudgetCache) RefundSpend(ctx context.Context, entityType, entityID string, amount float64) error {
	return bc.adjustSpend(ctx, entityType, entityID, -amount)
}

// UpdateBudgetCache updates cached budget status.
// If Redis already has a higher spent (from live reservations), that value wins so the
// async worker cannot roll back in-flight spend and allow overspend.
func (bc *BudgetCache) UpdateBudgetCache(ctx context.Context, entityType, entityID string, available, spent, limit float64, isExceeded bool) error {
	if existingSpent, err := bc.client.Get(ctx, bc.spentKey(entityType, entityID)).Float64(); err == nil {
		if existingSpent > spent {
			spent = existingSpent
			if limit > 0 {
				available = limit - spent
			}
			isExceeded = available <= 0
		}
	}

	status := &BudgetStatus{
		EntityID:    entityID,
		EntityType:  entityType,
		Available:   available,
		Spent:       spent,
		Limit:       limit,
		IsExceeded:  isExceeded,
		LastUpdated: time.Now(),
		TTL:         int64(bc.ttl.Seconds()),
	}
	if limit > 0 {
		status.Percentage = (spent / limit) * 100
	}
	return bc.writeBudgetStatus(ctx, entityType, entityID, status)
}

// UpdateBudgetCacheFromSnapshot writes a full snapshot (including unlimited budgets).
func (bc *BudgetCache) UpdateBudgetCacheFromSnapshot(ctx context.Context, entityType, entityID string, snap *BudgetSnapshot) error {
	if snap == nil {
		return fmt.Errorf("nil budget snapshot")
	}
	status := &BudgetStatus{
		EntityID:    entityID,
		EntityType:  entityType,
		Available:   snap.Available,
		Spent:       snap.Spent,
		Limit:       snap.Limit,
		Unlimited:   snap.Unlimited,
		IsExceeded:  !snap.Unlimited && snap.Limit > 0 && (snap.Available <= 0 || snap.Spent >= snap.Limit),
		LastUpdated: time.Now(),
		TTL:         int64(bc.ttl.Seconds()),
	}
	if snap.Limit > 0 {
		status.Percentage = (snap.Spent / snap.Limit) * 100
	}
	return bc.writeBudgetStatus(ctx, entityType, entityID, status)
}

func (bc *BudgetCache) writeBudgetStatus(ctx context.Context, entityType, entityID string, status *BudgetStatus) error {
	if status.Limit > 0 && status.Percentage == 0 && status.Spent > 0 {
		status.Percentage = (status.Spent / status.Limit) * 100
	}

	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal budget status: %w", err)
	}

	pipe := bc.client.Pipeline()
	pipe.SetEx(ctx, bc.budgetKey(entityType, entityID), data, bc.ttl)
	pipe.SetEx(ctx, bc.availableKey(entityType, entityID), formatFloat(status.Available), bc.ttl)
	pipe.SetEx(ctx, bc.spentKey(entityType, entityID), formatFloat(status.Spent), bc.ttl)
	pipe.SetEx(ctx, bc.limitKey(entityType, entityID), formatFloat(status.Limit), bc.ttl)
	if status.Unlimited {
		pipe.SetEx(ctx, bc.unlimitedKey(entityType, entityID), "1", bc.ttl)
	} else {
		pipe.Del(ctx, bc.unlimitedKey(entityType, entityID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		bc.logger.Error("Failed to update budget cache",
			zap.Error(err),
			zap.String("entity_type", entityType),
			zap.String("entity_id", entityID))
		return err
	}

	bc.logger.Debug("Budget cache updated",
		zap.String("entity_type", entityType),
		zap.String("entity_id", entityID),
		zap.Float64("available", status.Available),
		zap.Float64("spent", status.Spent),
		zap.Bool("unlimited", status.Unlimited))

	return nil
}

// IncrementSpent atomically increments spent and decrements available.
func (bc *BudgetCache) IncrementSpent(ctx context.Context, entityType, entityID string, amount float64) error {
	return bc.adjustSpend(ctx, entityType, entityID, amount)
}

func (bc *BudgetCache) adjustSpend(ctx context.Context, entityType, entityID string, amount float64) error {
	if amount == 0 {
		return nil
	}

	if _, err := bc.ensureBudgetStatus(ctx, entityType, entityID); err != nil {
		return err
	}

	ttlSec := int(bc.ttl.Seconds())
	if ttlSec <= 0 {
		ttlSec = 300
	}

	result, err := adjustSpendScript.Run(ctx, bc.client, []string{
		bc.availableKey(entityType, entityID),
		bc.spentKey(entityType, entityID),
		bc.unlimitedKey(entityType, entityID),
	}, amount, ttlSec).Int()
	if err != nil {
		return fmt.Errorf("failed to adjust spent amount: %w", err)
	}
	if result == -1 {
		if _, err := bc.ensureBudgetStatus(ctx, entityType, entityID); err != nil {
			return err
		}
		result, err = adjustSpendScript.Run(ctx, bc.client, []string{
			bc.availableKey(entityType, entityID),
			bc.spentKey(entityType, entityID),
			bc.unlimitedKey(entityType, entityID),
		}, amount, ttlSec).Int()
		if err != nil {
			return fmt.Errorf("failed to adjust spent amount: %w", err)
		}
		if result == -1 {
			return ErrBudgetUnavailable
		}
	}

	bc.syncStatusJSON(ctx, entityType, entityID)
	bc.logger.Debug("Budget spent adjusted",
		zap.String("entity_type", entityType),
		zap.String("entity_id", entityID),
		zap.Float64("amount", amount))
	return nil
}

// InvalidateBudgetCache removes cached budget status
func (bc *BudgetCache) InvalidateBudgetCache(ctx context.Context, entityType, entityID string) error {
	pipe := bc.client.Pipeline()
	pipe.Del(ctx, bc.budgetKey(entityType, entityID))
	pipe.Del(ctx, bc.availableKey(entityType, entityID))
	pipe.Del(ctx, bc.spentKey(entityType, entityID))
	pipe.Del(ctx, bc.limitKey(entityType, entityID))
	pipe.Del(ctx, bc.unlimitedKey(entityType, entityID))

	if _, err := pipe.Exec(ctx); err != nil {
		bc.logger.Error("Failed to invalidate budget cache",
			zap.Error(err),
			zap.String("entity_type", entityType),
			zap.String("entity_id", entityID))
		return err
	}

	bc.logger.Debug("Budget cache invalidated",
		zap.String("entity_type", entityType),
		zap.String("entity_id", entityID))
	return nil
}

// GetBudgetStats returns current budget statistics from cache (float keys are source of truth).
func (bc *BudgetCache) GetBudgetStats(ctx context.Context, entityType, entityID string) (*BudgetStatus, error) {
	return bc.readLiveStatus(ctx, entityType, entityID)
}

// ensureBudgetStatus returns cached status, loading from the DB on miss and populating Redis.
func (bc *BudgetCache) ensureBudgetStatus(ctx context.Context, entityType, entityID string) (*BudgetStatus, error) {
	if status, err := bc.readLiveStatus(ctx, entityType, entityID); err != nil {
		return nil, err
	} else if status != nil {
		return status, nil
	}

	bc.logger.Info("Budget cache miss, loading from source of truth",
		zap.String("entity_type", entityType),
		zap.String("entity_id", entityID))

	if bc.loader == nil {
		return nil, ErrBudgetLoaderMissing
	}

	snap, err := bc.loader.LoadBudget(ctx, entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBudgetUnavailable, err)
	}

	status := &BudgetStatus{
		EntityID:    entityID,
		EntityType:  entityType,
		Available:   snap.Available,
		Spent:       snap.Spent,
		Limit:       snap.Limit,
		Unlimited:   snap.Unlimited,
		IsExceeded:  !snap.Unlimited && snap.Limit > 0 && (snap.Available <= 0 || snap.Spent >= snap.Limit),
		LastUpdated: time.Now(),
		TTL:         int64(bc.ttl.Seconds()),
	}
	if snap.Limit > 0 {
		status.Percentage = (snap.Spent / snap.Limit) * 100
	}

	// SET NX on available key so concurrent pods don't clobber live reservations
	ok, err := bc.client.SetNX(ctx, bc.availableKey(entityType, entityID), formatFloat(status.Available), bc.ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBudgetUnavailable, err)
	}
	if ok {
		pipe := bc.client.Pipeline()
		pipe.SetEx(ctx, bc.spentKey(entityType, entityID), formatFloat(status.Spent), bc.ttl)
		pipe.SetEx(ctx, bc.limitKey(entityType, entityID), formatFloat(status.Limit), bc.ttl)
		if status.Unlimited {
			pipe.SetEx(ctx, bc.unlimitedKey(entityType, entityID), "1", bc.ttl)
		}
		data, _ := json.Marshal(status)
		pipe.SetEx(ctx, bc.budgetKey(entityType, entityID), data, bc.ttl)
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBudgetUnavailable, err)
		}
		return status, nil
	}

	// Another pod won — read live keys
	status, err = bc.readLiveStatus(ctx, entityType, entityID)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, ErrBudgetUnavailable
	}
	return status, nil
}

func (bc *BudgetCache) readLiveStatus(ctx context.Context, entityType, entityID string) (*BudgetStatus, error) {
	availStr, err := bc.client.Get(ctx, bc.availableKey(entityType, entityID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBudgetUnavailable, err)
	}

	available, err := strconv.ParseFloat(availStr, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid available value", ErrBudgetUnavailable)
	}

	spent, _ := bc.client.Get(ctx, bc.spentKey(entityType, entityID)).Float64()
	limit, _ := bc.client.Get(ctx, bc.limitKey(entityType, entityID)).Float64()
	unlimited, _ := bc.client.Get(ctx, bc.unlimitedKey(entityType, entityID)).Result()

	status := &BudgetStatus{
		EntityID:    entityID,
		EntityType:  entityType,
		Available:   available,
		Spent:       spent,
		Limit:       limit,
		Unlimited:   unlimited == "1",
		IsExceeded:  unlimited != "1" && available <= 0,
		LastUpdated: time.Now(),
		TTL:         int64(bc.ttl.Seconds()),
	}
	if limit > 0 {
		status.Percentage = (spent / limit) * 100
	}
	return status, nil
}

// syncStatusJSON best-effort mirrors float keys into the legacy JSON blob for readers.
func (bc *BudgetCache) syncStatusJSON(ctx context.Context, entityType, entityID string) {
	status, err := bc.readLiveStatus(ctx, entityType, entityID)
	if err != nil || status == nil {
		return
	}
	data, err := json.Marshal(status)
	if err != nil {
		return
	}
	_ = bc.client.SetEx(ctx, bc.budgetKey(entityType, entityID), data, bc.ttl).Err()
}

func (bc *BudgetCache) budgetKey(entityType, entityID string) string {
	return fmt.Sprintf("budget:%s:%s", entityType, entityID)
}

func (bc *BudgetCache) availableKey(entityType, entityID string) string {
	return fmt.Sprintf("budget:%s:%s:available", entityType, entityID)
}

func (bc *BudgetCache) spentKey(entityType, entityID string) string {
	return fmt.Sprintf("budget:%s:%s:spent", entityType, entityID)
}

func (bc *BudgetCache) limitKey(entityType, entityID string) string {
	return fmt.Sprintf("budget:%s:%s:limit", entityType, entityID)
}

func (bc *BudgetCache) unlimitedKey(entityType, entityID string) string {
	return fmt.Sprintf("budget:%s:%s:unlimited", entityType, entityID)
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// SetupBudgetLimits initializes budget limits in Redis for fast access
func (bc *BudgetCache) SetupBudgetLimits(ctx context.Context, limits map[string]map[string]float64) error {
	for entityType, entities := range limits {
		for entityID, limit := range entities {
			status := &BudgetStatus{
				EntityID:    entityID,
				EntityType:  entityType,
				Available:   limit,
				Spent:       0,
				Limit:       limit,
				Percentage:  0,
				IsExceeded:  false,
				LastUpdated: time.Now(),
				TTL:         int64(bc.ttl.Seconds()),
			}
			if err := bc.writeBudgetStatus(ctx, entityType, entityID, status); err != nil {
				return err
			}
		}
	}

	bc.logger.Info("Budget limits initialized in Redis",
		zap.Int("entity_types", len(limits)))
	return nil
}
