package syncer

import (
	"context"
	"strings"
	"testing"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

type fakeSource struct {
	days map[string]domain.DaySummary
}

func (f *fakeSource) DaySummary(_ context.Context, date string) (domain.DaySummary, error) {
	if day, ok := f.days[date]; ok {
		return day, nil
	}
	return domain.DaySummary{Date: date}, nil
}

type fakeSink struct {
	existing []domain.Point
	created  []domain.Point
	patched  []string
	deleted  map[domain.PointType][]string
}

func (f *fakeSink) List(_ context.Context, t domain.PointType, _, _ string) ([]domain.Point, error) {
	var points []domain.Point
	for _, p := range f.existing {
		if p.Type == t {
			points = append(points, p)
		}
	}
	return points, nil
}

func (f *fakeSink) Create(_ context.Context, p domain.Point) error {
	f.created = append(f.created, p)
	return nil
}

func (f *fakeSink) Patch(_ context.Context, name string, _ domain.Point) error {
	f.patched = append(f.patched, name)
	return nil
}

func (f *fakeSink) Delete(_ context.Context, t domain.PointType, names []string) error {
	if f.deleted == nil {
		f.deleted = map[domain.PointType][]string{}
	}
	f.deleted[t] = append(f.deleted[t], names...)
	return nil
}

func day(date string, lunchKcal, waterML float64) domain.DaySummary {
	return domain.DaySummary{
		Date: date,
		Meals: map[domain.Meal]domain.Macros{
			domain.Lunch: {EnergyKcal: lunchKcal, CarbsG: 17, ProteinG: 27, FatG: 27},
		},
		WaterML: waterML,
	}
}

func existingLunch(date string, kcal float64) domain.Point {
	id := "yazio-" + date + "-lunch"
	return domain.Point{
		ID:     id,
		Name:   "users/me/dataTypes/nutrition-log/dataPoints/" + id,
		Type:   domain.NutritionPoint,
		Date:   date,
		Meal:   domain.Lunch,
		Macros: domain.Macros{EnergyKcal: kcal, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
}

func TestRunFirstSyncCreatesEverything(t *testing.T) {
	sink := &fakeSink{}
	s := &Syncer{
		Source: &fakeSource{days: map[string]domain.DaySummary{
			"2026-06-09": day("2026-06-09", 500, 1000),
			"2026-06-10": day("2026-06-10", 408, 1650),
		}},
		Sink: sink,
	}

	stats, err := s.Run(context.Background(), "2026-06-09", "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}

	if stats.Created != 4 || stats.Patched != 0 || stats.Deleted != 0 {
		t.Errorf("stats = %s, want created=4 only", stats)
	}
	if len(sink.created) != 4 {
		t.Errorf("created %d points, want 4", len(sink.created))
	}
}

func TestRunSecondSyncIsIdempotent(t *testing.T) {
	sink := &fakeSink{existing: []domain.Point{
		existingLunch("2026-06-10", 408),
		{
			ID:      "yazio-2026-06-10-water",
			Name:    "users/me/dataTypes/hydration-log/dataPoints/yazio-2026-06-10-water",
			Type:    domain.HydrationPoint,
			Date:    "2026-06-10",
			WaterML: 1650,
		},
	}}
	s := &Syncer{
		Source: &fakeSource{days: map[string]domain.DaySummary{
			"2026-06-10": day("2026-06-10", 408, 1650),
		}},
		Sink: sink,
	}

	stats, err := s.Run(context.Background(), "2026-06-10", "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}

	if stats.Skipped != 2 || stats.Created != 0 || stats.Patched != 0 || stats.Deleted != 0 {
		t.Errorf("stats = %s, want skipped=2 only", stats)
	}
	if len(sink.created) != 0 {
		t.Errorf("created %d points on identical re-run, want 0", len(sink.created))
	}
}

func TestRunPatchesChanged(t *testing.T) {
	sink := &fakeSink{existing: []domain.Point{
		existingLunch("2026-06-10", 408), // value will change -> patch
		{ // water no longer in Yazio: left untouched (cannot identify ownership)
			ID:      "9999999999",
			Name:    "users/me/dataTypes/hydration-log/dataPoints/9999999999",
			Type:    domain.HydrationPoint,
			Date:    "2026-06-10",
			WaterML: 500,
		},
	}}
	s := &Syncer{
		Source: &fakeSource{days: map[string]domain.DaySummary{
			"2026-06-10": day("2026-06-10", 520, 0),
		}},
		Sink: sink,
	}

	stats, err := s.Run(context.Background(), "2026-06-10", "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}

	if stats.Patched != 1 || stats.Deleted != 0 || stats.Created != 0 {
		t.Errorf("stats = %s, want patched=1 only", stats)
	}
	if len(sink.patched) != 1 || !strings.HasSuffix(sink.patched[0], "yazio-2026-06-10-lunch") {
		t.Errorf("patched = %v", sink.patched)
	}
	if len(sink.deleted) != 0 {
		t.Errorf("unexpected deletes: %v", sink.deleted)
	}
}

func TestRunDryRunWritesNothing(t *testing.T) {
	sink := &fakeSink{existing: []domain.Point{existingLunch("2026-06-10", 408)}}
	var log strings.Builder
	s := &Syncer{
		Source: &fakeSource{days: map[string]domain.DaySummary{
			"2026-06-10": day("2026-06-10", 520, 1650),
		}},
		Sink:   sink,
		DryRun: true,
		Out:    &log,
	}

	stats, err := s.Run(context.Background(), "2026-06-10", "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}

	if stats.Created != 1 || stats.Patched != 1 {
		t.Errorf("stats = %s, want created=1 patched=1 (planned only)", stats)
	}
	if len(sink.created) != 0 || len(sink.patched) != 0 || len(sink.deleted) != 0 {
		t.Error("dry run must not write")
	}
	if !strings.Contains(log.String(), "create yazio-2026-06-10-water") {
		t.Errorf("log missing planned create:\n%s", log.String())
	}
}

func TestRunRejectsInvalidRange(t *testing.T) {
	s := &Syncer{Source: &fakeSource{}, Sink: &fakeSink{}}
	if _, err := s.Run(context.Background(), "2026-06-10", "2026-06-09"); err == nil {
		t.Error("expected error for reversed range")
	}
	if _, err := s.Run(context.Background(), "junk", "2026-06-09"); err == nil {
		t.Error("expected error for malformed date")
	}
}
