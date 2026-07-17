package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/jigar225/goget/internal/core/models"
)

// CachedAuthService wraps AuthService with caching for virtual key validation.
type CachedAuthService struct {
	authService *AuthService
	cache       *simpleCache
	logger      *zap.Logger
}

type simpleCache struct {
	data   map[string]cacheItem
	mutex  sync.RWMutex
	logger *zap.Logger
}

type cacheItem struct {
	value     interface{}
	expiresAt time.Time
}

func NewCachedAuthService(authService *AuthService, logger *zap.Logger) *CachedAuthService {
	cache := &simpleCache{
		data:   make(map[string]cacheItem),
		logger: logger,
	}
	go cache.cleanup()
	return &CachedAuthService{
		authService: authService,
		cache:       cache,
		logger:      logger,
	}
}

func (c *CachedAuthService) ValidateKeyCached(ctx context.Context, keyValue string) (*models.Key, error) {
	cacheKey := fmt.Sprintf("key:%s", models.HashKey(keyValue))

	if cached, found := c.cache.get(cacheKey); found {
		if keyData, ok := cached.(*models.Key); ok {
			return keyData, nil
		}
	}

	key, err := c.authService.ValidateKey(ctx, keyValue)
	if err != nil {
		return nil, err
	}

	c.cache.set(cacheKey, key, 5*time.Minute)
	return key, nil
}

func (c *CachedAuthService) InvalidateKeyCache(keyValue string) {
	c.cache.delete(fmt.Sprintf("key:%s", models.HashKey(keyValue)))
}

func (c *CachedAuthService) ClearCache() {
	c.cache.clear()
}

func (c *CachedAuthService) GetCacheStats() map[string]interface{} {
	return c.cache.stats()
}

func (c *simpleCache) get(key string) (interface{}, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	item, ok := c.data[key]
	if !ok || time.Now().After(item.expiresAt) {
		return nil, false
	}
	return item.value, true
}

func (c *simpleCache) set(key string, value interface{}, ttl time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.data[key] = cacheItem{value: value, expiresAt: time.Now().Add(ttl)}
}

func (c *simpleCache) delete(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.data, key)
}

func (c *simpleCache) clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.data = make(map[string]cacheItem)
}

func (c *simpleCache) stats() map[string]interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return map[string]interface{}{"entries": len(c.data)}
}

func (c *simpleCache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		c.mutex.Lock()
		now := time.Now()
		for key, item := range c.data {
			if now.After(item.expiresAt) {
				delete(c.data, key)
			}
		}
		c.mutex.Unlock()
	}
}
