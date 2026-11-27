package sqlite

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
	"github.com/demonidze1337/mia-lead-radar/internal/storage"
)

func TestSaveLeadIdempotentAndTelemetry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	radar := createTestRadar(t, store, "leads", true)

	lead := testLead(radar.ID, 1, 10)
	saved, inserted, err := store.SaveLead(ctx, lead)
	if err != nil {
		t.Fatalf("SaveLead: %v", err)
	}
	if !inserted || saved.ID <= 0 {
		t.Fatalf("first insert: %+v inserted=%v", saved, inserted)
	}
	if saved.Category != domain.LeadCategoryPotential {
		t.Fatalf("category = %q, want potential", saved.Category)
	}
	if saved.Status != domain.LeadStatusNew || saved.Lang != "en" {
		t.Fatalf("defaults missing: %+v", saved)
	}

	duplicate := testLead(radar.ID, 1, 10)
	duplicate.Text = "changed"
	again, inserted, err := store.SaveLead(ctx, duplicate)
	if err != nil {
		t.Fatalf("SaveLead duplicate: %v", err)
	}
	if inserted || again.ID != saved.ID || again.Text != lead.Text {
		t.Fatalf("duplicate handling: %+v inserted=%v", again, inserted)
	}

	second, inserted, err := store.SaveLead(ctx, testLead(radar.ID, 2, 11))
	if err != nil || !inserted {
		t.Fatalf("second lead: %+v inserted=%v err=%v", second, inserted, err)
	}
	third, _, err := store.SaveLead(ctx, testLead(radar.ID, 2, 12))
	if err != nil {
		t.Fatalf("third lead: %v", err)
	}

	all, total, err := store.ListLeads(ctx, storage.LeadFilter{})
	if err != nil {
		t.Fatalf("ListLeads: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("total=%d len=%d, want 3", total, len(all))
	}
	filtered, total, err := store.ListLeads(ctx, storage.LeadFilter{RadarID: radar.ID, MinScore: 70, Limit: 1})
	if err != nil {
		t.Fatalf("ListLeads filters: %v", err)
	}
	if total != 3 || len(filtered) != 1 {
		t.Fatalf("filter total=%d len=%d", total, len(filtered))
	}
	found, _, err := store.ListLeads(ctx, storage.LeadFilter{Query: "Buyer", Categories: []domain.LeadCategory{domain.LeadCategoryPotential}})
	if err != nil || len(found) != 3 {
		t.Fatalf("query result len=%d err=%v", len(found), err)
	}

	viewedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := store.MarkLeadViewed(ctx, saved.ID, viewedAt); err != nil {
		t.Fatalf("MarkLeadViewed: %v", err)
	}
	if err := store.MarkLeadViewed(ctx, saved.ID, viewedAt.Add(time.Hour)); err != nil {
		t.Fatalf("MarkLeadViewed second: %v", err)
	}
	openedAt := viewedAt.Add(time.Minute)
	if err := store.MarkLeadProfileOpened(ctx, saved.ID, openedAt); err != nil {
		t.Fatalf("MarkLeadProfileOpened: %v", err)
	}
	detailed, err := store.GetLeadByID(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetLeadByID: %v", err)
	}
	if detailed.ViewedAt == nil || !detailed.ViewedAt.Equal(viewedAt) {
		t.Fatalf("viewed at = %v, want %v", detailed.ViewedAt, viewedAt)
	}
	if detailed.ProfileOpenedAt == nil || !detailed.ProfileOpenedAt.Equal(openedAt) {
		t.Fatalf("profile opened at = %v", detailed.ProfileOpenedAt)
	}

	if err := store.AppendLeadIcebreaker(ctx, saved.ID, "Hi, saw your post", 90); err != nil {
		t.Fatalf("AppendLeadIcebreaker: %v", err)
	}
	if err := store.AppendLeadIcebreaker(ctx, saved.ID, "Second variant", 30); err != nil {
		t.Fatalf("AppendLeadIcebreaker second: %v", err)
	}
	detailed, _ = store.GetLeadByID(ctx, saved.ID)
	if detailed.IcebreakerCount != 2 || len(detailed.Icebreakers) != 2 || detailed.SavedTimeSeconds != 120 {
		t.Fatalf("unexpected icebreakers: %+v", detailed)
	}
	if detailed.Icebreakers[0] != "Hi, saw your post" || detailed.Icebreakers[1] != "Second variant" {
		t.Fatalf("icebreaker order: %+v", detailed.Icebreakers)
	}

	if err := store.UpdateLeadStatus(ctx, second.ID, domain.LeadStatusHidden); err != nil {
		t.Fatalf("UpdateLeadStatus: %v", err)
	}
	visible, total, err := store.ListLeads(ctx, storage.LeadFilter{})
	if err != nil {
		t.Fatalf("ListLeads visible: %v", err)
	}
	if total != 2 || len(visible) != 2 {
		t.Fatalf("hidden lead leaked: total=%d len=%d", total, len(visible))
	}
	hidden, total, err := store.ListLeads(ctx, storage.LeadFilter{IncludeHidden: true})
	if err != nil || total != 3 || len(hidden) != 3 {
		t.Fatalf("IncludeHidden: total=%d len=%d err=%v", total, len(hidden), err)
	}
	explicitHidden, total, err := store.ListLeads(ctx, storage.LeadFilter{Statuses: []domain.LeadStatus{domain.LeadStatusHidden}})
	if err != nil || total != 1 || len(explicitHidden) != 1 {
		t.Fatalf("explicit hidden: total=%d len=%d err=%v", total, len(explicitHidden), err)
	}
	if third.ID == 0 {
		t.Fatal("third lead was not saved")
	}

	for _, id := range []int64{9999} {
		if err := store.UpdateLeadStatus(ctx, id, domain.LeadStatusClosed); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("missing lead status err = %v", err)
		}
		if err := store.MarkLeadViewed(ctx, id, viewedAt); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("missing lead viewed err = %v", err)
		}
		if err := store.AppendLeadIcebreaker(ctx, id, "x", 1); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("missing lead icebreaker err = %v", err)
		}
	}
	if _, err := store.GetLeadByID(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing lead err = %v", err)
	}
}

func TestTrashSamplesPruneAndList(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	radar := createTestRadar(t, store, "trash", true)
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		err := store.SaveTrashSample(ctx, domain.TrashSample{
			RadarID:     radar.ID,
			Source:      "tier0",
			Reason:      "spam",
			MatchedTags: []string{"website"},
			Score:       i,
			ChatTitle:   "chat",
			AuthorID:    int64(i),
			TextExcerpt: "excerpt",
			Lang:        "en",
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
		}, 2)
		if err != nil {
			t.Fatalf("SaveTrashSample %d: %v", i, err)
		}
	}
	samples, err := store.ListTrashSamples(ctx, radar.ID, 10)
	if err != nil {
		t.Fatalf("ListTrashSamples: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(samples))
	}
	if samples[0].Score != 4 || samples[1].Score != 3 {
		t.Fatalf("unexpected pruned samples: %+v", samples)
	}
	if len(samples[0].MatchedTags) != 1 {
		t.Fatalf("matched tags not decoded: %+v", samples[0])
	}
	if err := store.SaveTrashSample(ctx, domain.TrashSample{}, 10); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("missing radar err = %v", err)
	}
}

func TestTagStatsThrottleAndFunnel(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	radar := createTestRadar(t, store, "metrics", true)
	now := time.Now().UTC()

	for i := 0; i < 4; i++ {
		if err := store.RecordTagHit(ctx, radar.ID, "website", false, false, now); err != nil {
			t.Fatalf("RecordTagHit trash: %v", err)
		}
	}
	if err := store.RecordTagHit(ctx, radar.ID, "website", true, false, now); err != nil {
		t.Fatalf("RecordTagHit lead: %v", err)
	}
	if err := store.RecordTagHit(ctx, radar.ID, "website", true, true, now); err != nil {
		t.Fatalf("RecordTagHit false positive: %v", err)
	}
	if err := store.RecordTagHit(ctx, radar.ID, "landing", true, false, now); err != nil {
		t.Fatalf("RecordTagHit landing: %v", err)
	}
	throttleUntil := now.Add(time.Hour).Truncate(time.Microsecond)
	if err := store.SetTagThrottle(ctx, radar.ID, "website", &throttleUntil); err != nil {
		t.Fatalf("SetTagThrottle: %v", err)
	}
	stats, err := store.ListTagStats(ctx, radar.ID)
	if err != nil {
		t.Fatalf("ListTagStats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %d, want 2", len(stats))
	}
	if stats[0].Tag != "website" || stats[0].TotalHits != 6 || stats[0].TrashHits != 4 ||
		stats[0].LeadHits != 2 || stats[0].FalsePositiveHits != 1 {
		t.Fatalf("unexpected website stats: %+v", stats[0])
	}
	if stats[0].ThrottledUntil == nil || !stats[0].ThrottledUntil.Equal(throttleUntil) {
		t.Fatalf("throttle = %v", stats[0].ThrottledUntil)
	}

	if err := store.IncrementDailyMetric(ctx, time.Now().UTC().Format("2006-01-02"), radar.ID, domain.DailyMetric{
		MessagesSeen:       50,
		Tier0Passed:        20,
		Tier0Rejected:      30,
		LLMCalled:          10,
		TrashCount:         8,
		DoubtfulCount:      1,
		PotentialCount:     1,
		ClearCount:         0,
		ViewedCount:        1,
		ProfileOpenedCount: 1,
		SavedTimeSeconds:   300,
	}); err != nil {
		t.Fatalf("IncrementDailyMetric: %v", err)
	}
	if err := store.IncrementDailyMetric(ctx, time.Now().UTC().Format("2006-01-02"), radar.ID, domain.DailyMetric{
		MessagesSeen: 5,
		TrashCount:   2,
	}); err != nil {
		t.Fatalf("IncrementDailyMetric second: %v", err)
	}

	summary, err := store.GetFunnelSummary(ctx, radar.ID, 7, 75)
	if err != nil {
		t.Fatalf("GetFunnelSummary: %v", err)
	}
	if summary.MessagesSeen != 55 || summary.TrashCount != 10 || summary.TotalLeads != 2 {
		t.Fatalf("unexpected summary counts: %+v", summary)
	}
	if math.Abs(summary.TrashRatioPct-10.0/12.0*100) > 1e-9 {
		t.Fatalf("trash ratio = %v", summary.TrashRatioPct)
	}
	if summary.ViewConversionPct != 50 || summary.ProfileConversionPct != 50 {
		t.Fatalf("conversions = %v / %v", summary.ViewConversionPct, summary.ProfileConversionPct)
	}
	if !summary.NoiseGuardTriggered {
		t.Error("noise guard should trigger at 83% trash")
	}
	if len(summary.NoisyTags) != 1 || summary.NoisyTags[0].Tag != "website" {
		t.Fatalf("noisy tags = %+v", summary.NoisyTags)
	}

	quiet, err := store.GetFunnelSummary(ctx, radar.ID, 7, 95)
	if err != nil {
		t.Fatalf("GetFunnelSummary quiet: %v", err)
	}
	if quiet.NoiseGuardTriggered {
		t.Error("noise guard triggered below threshold")
	}

	metrics, err := store.ListDailyMetrics(ctx, radar.ID, 7)
	if err != nil {
		t.Fatalf("ListDailyMetrics: %v", err)
	}
	if len(metrics) != 1 || metrics[0].MessagesSeen != 55 || metrics[0].TrashCount != 10 {
		t.Fatalf("unexpected daily metrics: %+v", metrics)
	}
	allMetrics, err := store.ListDailyMetrics(ctx, 0, 7)
	if err != nil || len(allMetrics) != 1 || allMetrics[0].MessagesSeen != 55 {
		t.Fatalf("all metrics: %+v err=%v", allMetrics, err)
	}

	if err := store.IncrementDailyMetric(ctx, "not-a-date", radar.ID, domain.DailyMetric{}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("invalid date err = %v", err)
	}
	if err := store.RecordTagHit(ctx, radar.ID, "", true, false, now); !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("empty tag err = %v", err)
	}
}
