package redis

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type stubLoader struct {
	snap *BudgetSnapshot
	err  error
	calls int
}

func (s *stubLoader) LoadBudget(ctx context.Context, entityType, entityID string) (*BudgetSnapshot, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.snap, nil
}

func TestBudgetCache(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	logger, _ := zap.NewDevelopment()
	cache := NewBudgetCache(client, logger, time.Minute)

	defer func() { client.FlushDB(ctx) }()

	t.Run("CheckBudgetAvailable_CacheMiss_NoLoader", func(t *testing.T) {
		available, err := cache.CheckBudgetAvailable(ctx, "user", "missing-user", 5.0)
		if !errors.Is(err, ErrBudgetLoaderMissing) {
			t.Errorf("Expected ErrBudgetLoaderMissing, got available=%v err=%v", available, err)
		}
		if available {
			t.Error("Expected deny on cache miss without loader")
		}
	})

	t.Run("CheckBudgetAvailable_CacheMiss_LoadsFromDB", func(t *testing.T) {
		loader := &stubLoader{snap: &BudgetSnapshot{Available: 40, Spent: 60, Limit: 100}}
		c := NewBudgetCache(client, logger, time.Minute).WithLoader(loader)

		available, err := c.CheckBudgetAvailable(ctx, "key", "load-key-1", 30.0)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !available {
			t.Error("Expected allow after DB load (40 >= 30)")
		}
		if loader.calls != 1 {
			t.Errorf("Expected 1 loader call, got %d", loader.calls)
		}

		// Second call should hit cache
		available, err = c.CheckBudgetAvailable(ctx, "key", "load-key-1", 50.0)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if available {
			t.Error("Expected deny (40 < 50)")
		}
		if loader.calls != 1 {
			t.Errorf("Expected still 1 loader call on cache hit, got %d", loader.calls)
		}
	})

	t.Run("UpdateAndCheckBudgetCache", func(t *testing.T) {
		err := cache.UpdateBudgetCache(ctx, "user", "test-user", 100.0, 25.0, 125.0, false)
		if err != nil {
			t.Errorf("Failed to update budget cache: %v", err)
		}

		available, err := cache.CheckBudgetAvailable(ctx, "user", "test-user", 50.0)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !available {
			t.Error("Expected budget to be available (100 - 50 >= 0)")
		}

		available, err = cache.CheckBudgetAvailable(ctx, "user", "test-user", 150.0)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if available {
			t.Error("Expected budget to be unavailable (100 - 150 < 0)")
		}
	})

	t.Run("IncrementSpent_UpdatesAvailable", func(t *testing.T) {
		err := cache.UpdateBudgetCache(ctx, "user", "test-user-2", 100.0, 0.0, 100.0, false)
		if err != nil {
			t.Errorf("Failed to update budget cache: %v", err)
		}

		err = cache.IncrementSpent(ctx, "user", "test-user-2", 25.5)
		if err != nil {
			t.Errorf("Failed to increment spent: %v", err)
		}

		status, err := cache.GetBudgetStats(ctx, "user", "test-user-2")
		if err != nil || status == nil {
			t.Fatalf("Failed to get status: %v", err)
		}
		if status.Spent != 25.5 {
			t.Errorf("Expected spent 25.5, got %f", status.Spent)
		}
		if status.Available != 74.5 {
			t.Errorf("Expected available 74.5, got %f", status.Available)
		}

		// Legacy spent key stays in sync
		spentKey := "budget:user:test-user-2:spent"
		spent, err := client.Get(ctx, spentKey).Float64()
		if err != nil {
			t.Errorf("Failed to get spent amount: %v", err)
		}
		if spent != 25.5 {
			t.Errorf("Expected spent key 25.5, got %f", spent)
		}

		available, err := cache.CheckBudgetAvailable(ctx, "user", "test-user-2", 80.0)
		if err != nil {
			t.Fatalf("check error: %v", err)
		}
		if available {
			t.Error("Expected deny after IncrementSpent reduced Available")
		}
	})

	t.Run("TryReserveBudget_Atomic", func(t *testing.T) {
		err := cache.UpdateBudgetCache(ctx, "key", "reserve-key", 10.0, 0.0, 10.0, false)
		if err != nil {
			t.Fatal(err)
		}

		ok, err := cache.TryReserveBudget(ctx, "key", "reserve-key", 6.0)
		if err != nil || !ok {
			t.Fatalf("expected reserve ok, got ok=%v err=%v", ok, err)
		}

		ok, err = cache.TryReserveBudget(ctx, "key", "reserve-key", 6.0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected second reserve of 6 to fail with 4 remaining")
		}

		status, _ := cache.GetBudgetStats(ctx, "key", "reserve-key")
		if status.Available != 4.0 || status.Spent != 6.0 {
			t.Errorf("expected avail=4 spent=6, got avail=%f spent=%f", status.Available, status.Spent)
		}

		if err := cache.RefundSpend(ctx, "key", "reserve-key", 6.0); err != nil {
			t.Fatal(err)
		}
		status, _ = cache.GetBudgetStats(ctx, "key", "reserve-key")
		if status.Available != 10.0 {
			t.Errorf("expected full refund to 10, got %f", status.Available)
		}
	})

	t.Run("TryReserveBudget_ConcurrentNoOverspend", func(t *testing.T) {
		err := cache.UpdateBudgetCache(ctx, "key", "concurrent-key", 50.0, 0.0, 50.0, false)
		if err != nil {
			t.Fatal(err)
		}

		const workers = 100
		const cost = 1.0
		var wg sync.WaitGroup
		var mu sync.Mutex
		allowed := 0

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := cache.TryReserveBudget(ctx, "key", "concurrent-key", cost)
				if err != nil {
					t.Errorf("reserve error: %v", err)
					return
				}
				if ok {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		if allowed != 50 {
			t.Errorf("expected exactly 50 allows, got %d", allowed)
		}
		status, _ := cache.GetBudgetStats(ctx, "key", "concurrent-key")
		if status.Available != 0 || status.Spent != 50 {
			t.Errorf("expected avail=0 spent=50, got avail=%f spent=%f", status.Available, status.Spent)
		}
	})

	t.Run("InvalidateBudgetCache", func(t *testing.T) {
		err := cache.UpdateBudgetCache(ctx, "user", "test-user-3", 100.0, 50.0, 150.0, false)
		if err != nil {
			t.Errorf("Failed to update budget cache: %v", err)
		}

		status, err := cache.GetBudgetStats(ctx, "user", "test-user-3")
		if err != nil {
			t.Errorf("Failed to get budget stats: %v", err)
		}
		if status == nil {
			t.Error("Expected budget status to exist")
		}

		err = cache.InvalidateBudgetCache(ctx, "user", "test-user-3")
		if err != nil {
			t.Errorf("Failed to invalidate cache: %v", err)
		}

		status, err = cache.GetBudgetStats(ctx, "user", "test-user-3")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if status != nil {
			t.Error("Expected budget status to be nil after invalidation")
		}
	})

	t.Run("SetupBudgetLimits", func(t *testing.T) {
		limits := map[string]map[string]float64{
			"user": {
				"user-1": 100.0,
				"user-2": 200.0,
			},
			"team": {
				"team-1": 500.0,
			},
		}

		err := cache.SetupBudgetLimits(ctx, limits)
		if err != nil {
			t.Errorf("Failed to setup budget limits: %v", err)
		}

		status, err := cache.GetBudgetStats(ctx, "user", "user-1")
		if err != nil {
			t.Errorf("Failed to get budget stats: %v", err)
		}
		if status == nil {
			t.Error("Expected budget status to exist")
			return
		}
		if status.Limit != 100.0 {
			t.Errorf("Expected limit to be 100.0, got %f", status.Limit)
		}
	})

	t.Run("UnlimitedBudget", func(t *testing.T) {
		loader := &stubLoader{snap: &BudgetSnapshot{Unlimited: true}}
		c := NewBudgetCache(client, logger, time.Minute).WithLoader(loader)

		ok, err := c.TryReserveBudget(ctx, "key", "unlimited-key", 999999)
		if err != nil || !ok {
			t.Fatalf("unlimited should always allow, ok=%v err=%v", ok, err)
		}
	})
}
