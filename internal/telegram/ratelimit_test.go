package telegram

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage/sqlite"
)

func testStore(t *testing.T) *sqlite.SQLiteStore {
	t.Helper()
	store, err := sqlite.New(filepath.Join(t.TempDir(), "telegram.db"))
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func TestRateGovernorBurstAndInterval(t *testing.T) {
	g := NewRateGovernor(map[OpKind]OpPolicy{
		OpJoin: {Burst: 2, Interval: 15 * time.Millisecond},
	}, nil)
	g.jitter = func(time.Duration) time.Duration { return 0 }

	start := time.Now()
	if err := g.Wait(context.Background(), 1, OpJoin); err != nil {
		t.Fatalf("first wait: %v", err)
	}
	if err := g.Wait(context.Background(), 1, OpJoin); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Fatalf("burst consumed too slowly: %v", elapsed)
	}
	if err := g.Wait(context.Background(), 1, OpJoin); err != nil {
		t.Fatalf("third wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Fatalf("third token was not paced: %v", elapsed)
	}
}

func TestRateGovernorWaitCancellation(t *testing.T) {
	g := NewRateGovernor(map[OpKind]OpPolicy{
		OpSearch: {Burst: 1, Interval: time.Hour},
	}, nil)
	g.jitter = func(time.Duration) time.Duration { return 0 }
	if err := g.Wait(context.Background(), 1, OpSearch); err != nil {
		t.Fatalf("prime token: %v", err)
	}
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Millisecond)
		err := g.Wait(ctx, 1, OpSearch)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wait %d err = %v, want deadline", i, err)
		}
	}
}

func TestFloodGatePersistenceAndRoundRobin(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	acc1, err := store.UpsertAccount(ctx, domain.TelegramAccount{Label: "one", TGUserID: 1, Status: domain.AccountStatusActive})
	if err != nil {
		t.Fatalf("UpsertAccount 1: %v", err)
	}
	acc2, err := store.UpsertAccount(ctx, domain.TelegramAccount{Label: "two", TGUserID: 2, Status: domain.AccountStatusActive})
	if err != nil {
		t.Fatalf("UpsertAccount 2: %v", err)
	}
	gate := NewFloodGate(store, 500*time.Millisecond, func() time.Time { return now })
	until, err := gate.RegisterFlood(ctx, acc1.ID, 2*time.Second, "FLOOD_WAIT_2")
	if err != nil {
		t.Fatalf("RegisterFlood: %v", err)
	}
	if !until.After(now) {
		t.Fatalf("until = %v", until)
	}
	if blocked, _ := gate.IsBlocked(acc1.ID); !blocked {
		t.Fatal("account not blocked after RegisterFlood")
	}
	if err := gate.CheckErr(acc1.ID); !errors.Is(err, domain.ErrFloodWait) {
		t.Fatalf("CheckErr = %v", err)
	}

	persisted := NewFloodGate(store, 0, func() time.Time { return now })
	if err := persisted.SyncFromStore(ctx); err != nil {
		t.Fatalf("SyncFromStore: %v", err)
	}
	if blocked, _ := persisted.IsBlocked(acc1.ID); !blocked {
		t.Fatal("flood state did not survive SyncFromStore")
	}

	accounts, err := store.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	first, ok := persisted.PickAvailableAccount(accounts)
	if !ok || first.ID != acc2.ID {
		t.Fatalf("first available = %+v ok=%v, want account 2", first, ok)
	}
	if _, ok := persisted.PickAvailableAccount([]domain.TelegramAccount{{ID: acc1.ID, Status: domain.AccountStatusActive}}); ok {
		t.Fatal("flooded account was selected")
	}

	// Round-robin across two healthy accounts.
	healthy := []domain.TelegramAccount{
		{ID: 10, Status: domain.AccountStatusActive},
		{ID: 11, Status: domain.AccountStatusActive},
	}
	rr := NewFloodGate(nil, 0, func() time.Time { return now })
	seen := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		acc, ok := rr.PickAvailableAccount(healthy)
		if !ok {
			t.Fatal("no account picked")
		}
		seen = append(seen, acc.ID)
	}
	if seen[0] != 10 || seen[1] != 11 || seen[2] != 10 {
		t.Fatalf("round robin = %v, want [10 11 10]", seen)
	}
}

func TestFloodGateExpiredCleanup(t *testing.T) {
	now := time.Now().UTC()
	store := testStore(t)
	ctx := context.Background()
	acc, err := store.UpsertAccount(ctx, domain.TelegramAccount{TGUserID: 5, Status: domain.AccountStatusActive})
	if err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	gate := NewFloodGate(store, 0, func() time.Time { return now })
	if _, err := gate.RegisterFlood(ctx, acc.ID, time.Second, ""); err != nil {
		t.Fatalf("RegisterFlood: %v", err)
	}
	gate.now = func() time.Time { return now.Add(2 * time.Second) }
	if blocked, _ := gate.IsBlocked(acc.ID); blocked {
		t.Fatal("expired flood still blocked")
	}
	if err := gate.CheckErr(acc.ID); err != nil {
		t.Fatalf("CheckErr expired = %v", err)
	}
}
