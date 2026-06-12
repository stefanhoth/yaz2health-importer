package planner

import (
	"testing"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

func nutritionPoint(id string, kcal float64) domain.Point {
	return domain.Point{
		ID:     id,
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   domain.Lunch,
		Macros: domain.Macros{EnergyKcal: kcal, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
}

// existingPoint simulates a point returned by Google Health: server-assigned
// numeric ID, full resource Name, same values as the desired point.
func existingPoint(serverID string, kcal float64) domain.Point {
	return domain.Point{
		ID:     serverID,
		Name:   "users/u1/dataTypes/nutrition-log/dataPoints/" + serverID,
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   domain.Lunch,
		Macros: domain.Macros{EnergyKcal: kcal, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
}

func TestPlanCreateWhenMissing(t *testing.T) {
	actions := Plan([]domain.Point{nutritionPoint("yazio-2026-06-10-lunch", 408)}, nil)

	if len(actions) != 1 || actions[0].Op != OpCreate {
		t.Fatalf("got %+v, want one create", actions)
	}
	if actions[0].Desired.ID != "yazio-2026-06-10-lunch" {
		t.Errorf("Desired.ID = %q", actions[0].Desired.ID)
	}
}

func TestPlanSkipWhenIdentical(t *testing.T) {
	actions := Plan(
		[]domain.Point{nutritionPoint("yazio-2026-06-10-lunch", 408)},
		[]domain.Point{existingPoint("1234567890", 408)},
	)

	if len(actions) != 1 || actions[0].Op != OpSkip {
		t.Fatalf("got %+v, want one skip", actions)
	}
}

func TestPlanSkipWithinTolerance(t *testing.T) {
	actions := Plan(
		[]domain.Point{nutritionPoint("yazio-2026-06-10-lunch", 408)},
		[]domain.Point{existingPoint("1234567890", 408.005)},
	)

	if len(actions) != 1 || actions[0].Op != OpSkip {
		t.Fatalf("got %+v, want one skip (within 0.01 tolerance)", actions)
	}
}

func TestPlanPatchWhenValuesChanged(t *testing.T) {
	actions := Plan(
		[]domain.Point{nutritionPoint("yazio-2026-06-10-lunch", 520)},
		[]domain.Point{existingPoint("1234567890", 408)},
	)

	if len(actions) != 1 || actions[0].Op != OpPatch {
		t.Fatalf("got %+v, want one patch", actions)
	}
	if actions[0].Existing.Name == "" {
		t.Error("patch action lost the existing resource name")
	}
	if actions[0].Desired.Macros.EnergyKcal != 520 {
		t.Errorf("Desired kcal = %v, want 520", actions[0].Desired.Macros.EnergyKcal)
	}
}

func TestPlanLeavesUnmatchedExistingUntouched(t *testing.T) {
	// Points in Google Health that have no desired counterpart (e.g. logged by
	// another app, or removed in Yazio) are left untouched because we cannot
	// distinguish our own points from foreign ones without client ID ownership.
	unmatched := existingPoint("9999999999", 250)
	unmatched.Meal = domain.Dinner // different meal → no semantic match

	actions := Plan(nil, []domain.Point{unmatched})

	if len(actions) != 0 {
		t.Fatalf("got %+v, want no actions on unmatched existing points", actions)
	}
}

func mealPointFor(id string, meal domain.Meal, kcal float64) domain.Point {
	return domain.Point{
		ID:     id,
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   meal,
		Macros: domain.Macros{EnergyKcal: kcal, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
}

func existingMealPoint(serverID string, meal domain.Meal, kcal float64) domain.Point {
	return domain.Point{
		ID:     serverID,
		Name:   "users/u1/dataTypes/nutrition-log/dataPoints/" + serverID,
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   meal,
		Macros: domain.Macros{EnergyKcal: kcal, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
}

func TestPlanMixedDay(t *testing.T) {
	desired := []domain.Point{
		mealPointFor("yazio-2026-06-10-breakfast", domain.Breakfast, 326),
		mealPointFor("yazio-2026-06-10-lunch", domain.Lunch, 520),
		{ID: "yazio-2026-06-10-water", Type: domain.HydrationPoint, Date: "2026-06-10", WaterML: 1650},
	}
	existing := []domain.Point{
		existingMealPoint("1000000001", domain.Lunch, 408),   // changed → patch
		existingMealPoint("1000000002", domain.Dinner, 783),  // no desired counterpart → untouched
		{ID: "2000000001", Name: "users/u1/dataTypes/hydration-log/dataPoints/2000000001", Type: domain.HydrationPoint, Date: "2026-06-10", WaterML: 1650},
	}

	actions := Plan(desired, existing)

	got := map[string]Op{}
	for _, a := range actions {
		got[a.pointID()] = a.Op
	}
	want := map[string]Op{
		"yazio-2026-06-10-breakfast": OpCreate,
		"yazio-2026-06-10-lunch":     OpPatch,
		"yazio-2026-06-10-water":     OpSkip,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d actions %v, want %d", len(got), got, len(want))
	}
	for id, op := range want {
		if got[id] != op {
			t.Errorf("%s: op = %s, want %s", id, got[id], op)
		}
	}
}

func TestPlanIsStableAcrossRuns(t *testing.T) {
	desired := []domain.Point{
		nutritionPoint("yazio-2026-06-10-lunch", 408),
		nutritionPoint("yazio-2026-06-10-breakfast", 326),
	}
	a := Plan(desired, nil)
	b := Plan(desired, nil)
	for i := range a {
		if a[i].pointID() != b[i].pointID() {
			t.Errorf("ordering not stable: %s vs %s", a[i].pointID(), b[i].pointID())
		}
	}
}
