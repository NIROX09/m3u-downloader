package pause

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Thiritin/m3u-downloader/internal/store"
)

func TestPauseRulesAndScheduleEvaluation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	controller := New(st.DB())
	ctx := context.Background()

	now := time.Date(2026, time.January, 5, 21, 30, 0, 0, time.Local) // Monday
	rule, err := controller.Create(ctx, RuleInput{
		Scope: "both", Mode: "schedule", Days: []int{1},
		StartTime: "22:00", EndTime: "07:00",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if rule.Mode != "schedule" || len(rule.Days) != 1 || rule.Days[0] != 1 {
		t.Fatalf("unexpected schedule rule: %+v", rule)
	}

	_, state, err := controller.List(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if state.PausedDownloads || state.PausedIPTV {
		t.Fatalf("schedule should not yet be active: %+v", state)
	}
	_, state, err = controller.List(ctx, time.Date(2026, time.January, 5, 22, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if !state.PausedDownloads || !state.PausedIPTV {
		t.Fatalf("schedule should pause both scopes: %+v", state)
	}
	_, state, err = controller.List(ctx, time.Date(2026, time.January, 6, 6, 59, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if !state.PausedDownloads || !state.PausedIPTV {
		t.Fatalf("overnight schedule should remain active: %+v", state)
	}
	_, state, err = controller.List(ctx, time.Date(2026, time.January, 6, 7, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if state.PausedDownloads || state.PausedIPTV {
		t.Fatalf("schedule should end at 07:00: %+v", state)
	}

	if err := controller.Delete(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	rules, _, err := controller.List(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected deleted rule, got %d", len(rules))
	}
}

func TestPauseRuleValidation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_, err = New(st.DB()).Create(context.Background(), RuleInput{
		Scope: "iptv", Mode: "duration", DurationMinutes: 0,
	}, time.Now())
	if err == nil {
		t.Fatal("expected invalid duration")
	}
}
