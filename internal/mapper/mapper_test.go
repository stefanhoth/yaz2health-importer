package mapper

import (
	"testing"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

func fullDay() domain.DaySummary {
	return domain.DaySummary{
		Date: "2026-06-10",
		Meals: map[domain.Meal]domain.Macros{
			domain.Breakfast: {EnergyKcal: 326.5, CarbsG: 30.45, ProteinG: 21.7, FatG: 9.9},
			domain.Lunch:     {EnergyKcal: 408, CarbsG: 17, ProteinG: 27, FatG: 27},
			domain.Dinner:    {EnergyKcal: 783, CarbsG: 69, ProteinG: 26, FatG: 38},
			domain.Snack:     {EnergyKcal: 107, CarbsG: 21, ProteinG: 4, FatG: 0},
		},
		WaterML: 1650,
	}
}

func TestMapFullDay(t *testing.T) {
	points := Map(fullDay())

	if len(points) != 5 {
		t.Fatalf("got %d points, want 5 (4 meals + water)", len(points))
	}

	wantIDs := map[string]domain.PointType{
		"yazio-2026-06-10-breakfast": domain.NutritionPoint,
		"yazio-2026-06-10-lunch":     domain.NutritionPoint,
		"yazio-2026-06-10-snack":     domain.NutritionPoint,
		"yazio-2026-06-10-dinner":    domain.NutritionPoint,
		"yazio-2026-06-10-water":     domain.HydrationPoint,
	}
	for _, p := range points {
		wantType, ok := wantIDs[p.ID]
		if !ok {
			t.Errorf("unexpected point ID %q", p.ID)
			continue
		}
		if p.Type != wantType {
			t.Errorf("point %s: type = %s, want %s", p.ID, p.Type, wantType)
		}
		if p.Date != "2026-06-10" {
			t.Errorf("point %s: date = %s, want 2026-06-10", p.ID, p.Date)
		}
		delete(wantIDs, p.ID)
	}
	if len(wantIDs) > 0 {
		t.Errorf("missing points: %v", wantIDs)
	}
}

func TestMapIsDeterministic(t *testing.T) {
	a := Map(fullDay())
	b := Map(fullDay())
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("point %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestMapSkipsEmptyMealAndWater(t *testing.T) {
	day := fullDay()
	day.Meals[domain.Snack] = domain.Macros{}
	day.WaterML = 0

	points := Map(day)

	if len(points) != 3 {
		t.Fatalf("got %d points, want 3", len(points))
	}
	for _, p := range points {
		if p.ID == "yazio-2026-06-10-snack" || p.Type == domain.HydrationPoint {
			t.Errorf("unexpected point %q for empty data", p.ID)
		}
	}
}

func TestMapEmptyDay(t *testing.T) {
	if points := Map(domain.DaySummary{Date: "2026-06-10"}); len(points) != 0 {
		t.Errorf("got %d points for empty day, want 0", len(points))
	}
}

func TestMapWaterValues(t *testing.T) {
	points := Map(domain.DaySummary{Date: "2026-06-10", WaterML: 1650})
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
	if points[0].WaterML != 1650 {
		t.Errorf("WaterML = %v, want 1650", points[0].WaterML)
	}
}
