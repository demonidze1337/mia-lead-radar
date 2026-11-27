package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func testRadar(name string, active bool) domain.RadarProfile {
	return domain.RadarProfile{
		Name:            name,
		Description:     "test radar",
		Tags:            []domain.RadarTag{{Phrase: "website", Kind: domain.RadarTagRoot, Weight: 1, Enabled: true}},
		TargetLanguages: []string{"en", "ru"},
		IsActive:        active,
	}
}

func createTestRadar(t *testing.T, store *SQLiteStore, name string, active bool) domain.RadarProfile {
	t.Helper()
	radar, err := store.CreateRadar(context.Background(), testRadar(name, active), domain.RadarChangeManual, "created in test")
	if err != nil {
		t.Fatalf("CreateRadar(%s): %v", name, err)
	}
	return radar
}

func testLead(radarID, chatID int64, msgID int) domain.Lead {
	return domain.Lead{
		RadarID:          radarID,
		ChatID:           chatID,
		TGChatID:         1000 + chatID,
		ChatTitle:        "Test chat",
		MsgID:            msgID,
		MsgLink:          fmt.Sprintf("https://t.me/test/1000/%d", msgID),
		AuthorID:         42,
		AuthorName:       "Buyer",
		Text:             "I need a developer to build a website",
		Lang:             "en",
		Score:            72,
		MatchedTags:      []string{"website"},
		Tier0Score:       55,
		LLMModel:         "qwen",
		Icebreakers:      []string{},
		SavedTimeSeconds: 0,
	}
}

func TestMigrateIdempotentAndPragmas(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("schema_migrations count = %d, want 1", count)
	}
	var journalMode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}
	var foreignKeys, busyTimeout int
	if err := store.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}
	if err := store.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}
}

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	settings, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings defaults: %v", err)
	}
	if settings.UILanguage != "en" || settings.MinLeadScore != 31 || settings.LLM.FastModel != "qwen3.5:4b" {
		t.Fatalf("unexpected defaults: %+v", settings)
	}

	settings.UILanguage = "ru"
	settings.TargetLanguages = []string{"ru", "uk"}
	settings.LLM.Provider = domain.LLMProviderOpenRouter
	settings.LLM.APIKey = "secret"
	settings.AllowHRVacancies = true
	settings.NoiseTrashThresholdPct = 65
	if err := store.SaveSettings(ctx, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	loaded, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if loaded.UILanguage != "ru" || loaded.LLM.Provider != domain.LLMProviderOpenRouter || loaded.LLM.APIKey != "secret" {
		t.Fatalf("settings round-trip mismatch: %+v", loaded)
	}
	if !loaded.AllowHRVacancies || loaded.NoiseTrashThresholdPct != 65 {
		t.Fatalf("flags mismatch: %+v", loaded)
	}
}

func TestAuthSessions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sess := domain.AuthSession{
		TokenHash: "hash-1",
		Label:     "browser",
		TGUserID:  777,
		ExpiresAt: now.Add(time.Hour),
	}
	id, err := store.CreateAuthSession(ctx, sess)
	if err != nil {
		t.Fatalf("CreateAuthSession: %v", err)
	}
	if id <= 0 {
		t.Fatalf("id = %d", id)
	}
	loaded, err := store.GetAuthSessionByHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("GetAuthSessionByHash: %v", err)
	}
	if loaded.TGUserID != 777 || loaded.Role != "owner" || loaded.Label != "browser" {
		t.Fatalf("unexpected session: %+v", loaded)
	}
	seen := now.Add(2 * time.Minute)
	if err := store.TouchAuthSession(ctx, id, seen); err != nil {
		t.Fatalf("TouchAuthSession: %v", err)
	}
	loaded, _ = store.GetAuthSessionByHash(ctx, "hash-1")
	if !loaded.LastSeenAt.Equal(seen.Truncate(time.Microsecond)) {
		t.Errorf("last seen = %v, want %v", loaded.LastSeenAt, seen)
	}
	if err := store.TouchAuthSession(ctx, 9999, seen); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("touch missing session err = %v", err)
	}
	if _, err := store.CreateAuthSession(ctx, domain.AuthSession{TokenHash: "hash-1", ExpiresAt: now.Add(time.Hour)}); err == nil {
		t.Error("expected unique token hash error")
	}
	if _, err := store.CreateAuthSession(ctx, domain.AuthSession{TokenHash: "hash-2"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("missing expiry err = %v", err)
	}

	if _, err := store.CreateAuthSession(ctx, domain.AuthSession{
		TokenHash: "expired",
		ExpiresAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	removed, err := store.DeleteExpiredAuthSessions(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpiredAuthSessions: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, err := store.GetAuthSessionByHash(ctx, "expired"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expired session still present: %v", err)
	}
}

func TestWithTxRollbackAndNestedJoin(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	radar := createTestRadar(t, store, "tx", true)

	sentinel := errors.New("rollback requested")
	err := store.WithTx(ctx, func(tx storage.Store) error {
		if _, _, err := tx.SaveLead(ctx, testLead(radar.ID, 1, 1)); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v, want sentinel", err)
	}
	if _, total, err := store.ListLeads(ctx, storage.LeadFilter{}); err != nil || total != 0 {
		t.Fatalf("lead survived rollback: total=%d err=%v", total, err)
	}

	err = store.WithTx(ctx, func(tx storage.Store) error {
		return tx.WithTx(ctx, func(inner storage.Store) error {
			_, _, err := inner.SaveLead(ctx, testLead(radar.ID, 1, 1))
			return err
		})
	})
	if err != nil {
		t.Fatalf("nested WithTx: %v", err)
	}
	if _, total, _ := store.ListLeads(ctx, storage.LeadFilter{}); total != 1 {
		t.Fatalf("nested tx did not commit: total=%d", total)
	}

	// Inner failure must abort the outer transaction.
	err = store.WithTx(ctx, func(tx storage.Store) error {
		if err := tx.WithTx(ctx, func(inner storage.Store) error {
			if _, _, err := inner.SaveLead(ctx, testLead(radar.ID, 2, 2)); err != nil {
				return err
			}
			return sentinel
		}); err != nil {
			return err
		}
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("outer tx error = %v", err)
	}
	if _, total, _ := store.ListLeads(ctx, storage.LeadFilter{}); total != 1 {
		t.Fatalf("inner failure leaked rows: total=%d", total)
	}
}

func TestDedupFingerprints(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rec := storage.DedupRecord{
		Fingerprint: "fp-1",
		AuthorID:    42,
		TokenSig:    "sig",
		CreatedAt:   now,
		ExpiresAt:   now.Add(time.Hour),
	}
	if err := store.SaveDedupFingerprint(ctx, rec); err != nil {
		t.Fatalf("SaveDedupFingerprint: %v", err)
	}
	has, err := store.HasDedupFingerprint(ctx, "fp-1", now)
	if err != nil || !has {
		t.Fatalf("HasDedupFingerprint = %v, %v", has, err)
	}
	authorRecords, err := store.ListRecentAuthorFingerprints(ctx, 42, now, 10)
	if err != nil {
		t.Fatalf("ListRecentAuthorFingerprints: %v", err)
	}
	if len(authorRecords) != 1 || authorRecords[0].Fingerprint != "fp-1" || authorRecords[0].TokenSig != "sig" {
		t.Fatalf("unexpected author records: %+v", authorRecords)
	}
	expiredRec := storage.DedupRecord{
		Fingerprint: "fp-expired",
		AuthorID:    42,
		CreatedAt:   now.Add(-2 * time.Hour),
		ExpiresAt:   now.Add(-time.Hour),
	}
	if err := store.SaveDedupFingerprint(ctx, expiredRec); err != nil {
		t.Fatalf("save expired fingerprint: %v", err)
	}
	if has, _ := store.HasDedupFingerprint(ctx, "fp-expired", now); has {
		t.Error("expired fingerprint reported as present")
	}
	pruned, err := store.PruneExpiredDedup(ctx, now)
	if err != nil {
		t.Fatalf("PruneExpiredDedup: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
	if _, err := store.HasDedupFingerprint(ctx, " ", now); err == nil {
		t.Error("expected error for empty fingerprint")
	}
}

func TestConsultantMessages(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		msg, err := store.AppendConsultantMessage(ctx, domain.ConsultantMessage{
			SessionKey: "audit-1",
			Role:       "user",
			Mode:       "audit",
			Content:    fmt.Sprintf("message-%d", i),
		})
		if err != nil {
			t.Fatalf("AppendConsultantMessage: %v", err)
		}
		if msg.ID <= 0 || msg.CreatedAt.IsZero() {
			t.Fatalf("unexpected message: %+v", msg)
		}
	}
	messages, err := store.ListConsultantMessages(ctx, "audit-1", 2)
	if err != nil {
		t.Fatalf("ListConsultantMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len = %d, want 2", len(messages))
	}
	if messages[0].Content != "message-1" || messages[1].Content != "message-2" {
		t.Fatalf("unexpected order: %+v", messages)
	}
	if _, err := store.AppendConsultantMessage(ctx, domain.ConsultantMessage{Role: "user"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("empty session key err = %v", err)
	}
}

func TestConcurrentStoreOperations(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	radar := createTestRadar(t, store, "concurrent", true)

	const leadWorkers = 25
	var wg sync.WaitGroup
	errCh := make(chan error, leadWorkers+25+10)
	wg.Add(leadWorkers)
	for i := 0; i < leadWorkers; i++ {
		go func(msgID int) {
			defer wg.Done()
			if _, _, err := store.SaveLead(ctx, testLead(radar.ID, 5, msgID)); err != nil {
				errCh <- fmt.Errorf("save lead %d: %w", msgID, err)
			}
		}(i)
	}
	wg.Wait()

	if _, total, err := store.ListLeads(ctx, storage.LeadFilter{}); err != nil || total != leadWorkers {
		t.Fatalf("leads total = %d err = %v, want %d", total, err, leadWorkers)
	}

	const jobs = 10
	for i := 0; i < jobs; i++ {
		if _, _, err := store.EnqueueJoinJob(ctx, domain.JoinQueueItem{
			NormalizedLink: fmt.Sprintf("https://t.me/chat_%d", i),
			Priority:       i,
			NextAttemptAt:  time.Now().Add(-time.Minute),
		}); err != nil {
			t.Fatalf("EnqueueJoinJob: %v", err)
		}
	}
	claimed := make(chan int64, jobs)
	wg.Add(jobs)
	for i := 0; i < jobs; i++ {
		go func() {
			defer wg.Done()
			item, err := store.ClaimNextJoinJob(ctx, time.Now())
			if err != nil {
				errCh <- fmt.Errorf("claim: %w", err)
				return
			}
			if item != nil {
				claimed <- item.ID
			}
		}()
	}
	wg.Wait()
	close(claimed)
	unique := make(map[int64]struct{})
	for id := range claimed {
		unique[id] = struct{}{}
	}
	if len(unique) != jobs {
		t.Fatalf("claimed %d unique jobs, want %d", len(unique), jobs)
	}

	const metricWorkers = 25
	wg.Add(metricWorkers)
	for i := 0; i < metricWorkers; i++ {
		go func() {
			defer wg.Done()
			if err := store.IncrementDailyMetric(ctx, time.Now().UTC().Format("2006-01-02"), radar.ID, domain.DailyMetric{MessagesSeen: 1}); err != nil {
				errCh <- fmt.Errorf("metric: %w", err)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	summary, err := store.GetFunnelSummary(ctx, radar.ID, 1, 75)
	if err != nil {
		t.Fatalf("GetFunnelSummary: %v", err)
	}
	if summary.MessagesSeen != metricWorkers {
		t.Fatalf("messages seen = %d, want %d", summary.MessagesSeen, metricWorkers)
	}
}
