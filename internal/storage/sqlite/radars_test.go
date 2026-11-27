package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

func TestRadarLifecycleVersionsAndActivation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := createTestRadar(t, store, "first", true)
	if first.Version != 1 || !first.IsActive {
		t.Fatalf("unexpected created radar: %+v", first)
	}
	versions, err := store.ListRadarVersions(ctx, first.ID, 10)
	if err != nil {
		t.Fatalf("ListRadarVersions: %v", err)
	}
	if len(versions) != 1 || versions[0].Version != 1 || versions[0].ChangeSource != domain.RadarChangeManual {
		t.Fatalf("unexpected versions: %+v", versions)
	}

	second := createTestRadar(t, store, "second", true)
	reloadedFirst, err := store.GetRadarByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetRadarByID: %v", err)
	}
	if reloadedFirst.IsActive {
		t.Error("creating an active radar did not deactivate the previous one")
	}
	active, err := store.GetActiveRadar(ctx)
	if err != nil {
		t.Fatalf("GetActiveRadar: %v", err)
	}
	if active.ID != second.ID {
		t.Fatalf("active radar = %d, want %d", active.ID, second.ID)
	}

	second.Tags = append(second.Tags, domain.RadarTag{Phrase: "landing page", Kind: domain.RadarTagLive, Weight: 0.8, Enabled: true})
	second.QualityScore = 88
	second.QualityNotes = "good"
	updated, err := store.UpdateRadar(ctx, second, domain.RadarChangeAIAudit, "audit improved tags")
	if err != nil {
		t.Fatalf("UpdateRadar: %v", err)
	}
	if updated.Version != 2 || updated.QualityScore != 88 || len(updated.Tags) != 2 {
		t.Fatalf("unexpected updated radar: %+v", updated)
	}
	versions, err = store.ListRadarVersions(ctx, second.ID, 10)
	if err != nil {
		t.Fatalf("ListRadarVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %d, want 2", len(versions))
	}
	if versions[0].Version != 2 || versions[0].ChangeSource != domain.RadarChangeAIAudit || versions[0].AuditSummary != "audit improved tags" {
		t.Fatalf("unexpected newest version: %+v", versions[0])
	}
	if len(versions[1].Tags) != 1 {
		t.Fatalf("old snapshot was not preserved: %+v", versions[1])
	}

	all, err := store.ListRadars(ctx)
	if err != nil {
		t.Fatalf("ListRadars: %v", err)
	}
	if len(all) != 2 || !all[0].IsActive {
		t.Fatalf("unexpected radar list: %+v", all)
	}

	if _, err := store.GetRadarByID(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing radar err = %v", err)
	}
	if _, err := store.CreateRadar(ctx, domain.RadarProfile{}, domain.RadarChangeManual, ""); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("empty radar name err = %v", err)
	}
	if _, err := store.UpdateRadar(ctx, domain.RadarProfile{ID: 9999, Name: "x"}, domain.RadarChangeManual, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("update missing radar err = %v", err)
	}
}

func TestAddLearnedStopWordsIsIdempotentAndVersioned(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	radar := createTestRadar(t, store, "learn", true)

	updated, err := store.AddLearnedStopWords(ctx, radar.ID, []string{"spam", " Spam ", "", "scam"})
	if err != nil {
		t.Fatalf("AddLearnedStopWords: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("version = %d, want 2", updated.Version)
	}
	if len(updated.LearnedStopWords) != 2 || updated.LearnedStopWords[0] != "spam" || updated.LearnedStopWords[1] != "scam" {
		t.Fatalf("unexpected stop words: %+v", updated.LearnedStopWords)
	}
	versions, err := store.ListRadarVersions(ctx, radar.ID, 5)
	if err != nil {
		t.Fatalf("ListRadarVersions: %v", err)
	}
	if versions[0].Version != 2 || versions[0].ChangeSource != domain.RadarChangeAutoLearn {
		t.Fatalf("unexpected auto-learn snapshot: %+v", versions[0])
	}
	if len(versions[0].LearnedStopWords) != 2 {
		t.Fatalf("snapshot stop words = %+v", versions[0].LearnedStopWords)
	}

	same, err := store.AddLearnedStopWords(ctx, radar.ID, []string{"SPAM"})
	if err != nil {
		t.Fatalf("AddLearnedStopWords repeat: %v", err)
	}
	if same.Version != 2 {
		t.Fatalf("no-op update bumped version to %d", same.Version)
	}
	if _, err := store.AddLearnedStopWords(ctx, 9999, []string{"x"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing radar err = %v", err)
	}
}

func TestGetActiveRadarWhenNone(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.GetActiveRadar(context.Background()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
