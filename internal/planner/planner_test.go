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

func existingPoint(id string, kcal float64) domain.Point {
	p := nutritionPoint(id, kcal)
	p.Name = "users/u1/dataTypes/nutrition-log/dataPoints/" + id
	return p
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
		[]domain.Point{existingPoint("yazio-2026-06-10-lunch", 408)},
	)

	if len(actions) != 1 || actions[0].Op != OpSkip {
		t.Fatalf("got %+v, want one skip", actions)
	}
}

func TestPlanSkipWithinTolerance(t *testing.T) {
	actions := Plan(
		[]domain.Point{nutritionPoint("yazio-2026-06-10-lunch", 408)},
		[]domain.Point{existingPoint("yazio-2026-06-10-lunch", 408.005)},
	)

	if len(actions) != 1 || actions[0].Op != OpSkip {
		t.Fatalf("got %+v, want one skip (within 0.01 tolerance)", actions)
	}
}

func TestPlanPatchWhenValuesChanged(t *testing.T) {
	actions := Plan(
		[]domain.Point{nutritionPoint("yazio-2026-06-10-lunch", 520)},
		[]domain.Point{existingPoint("yazio-2026-06-10-lunch", 408)},
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

func TestPlanDeleteWhenRemovedInYazio(t *testing.T) {
	actions := Plan(nil, []domain.Point{existingPoint("yazio-2026-06-10-lunch", 408)})

	if len(actions) != 1 || actions[0].Op != OpDelete {
		t.Fatalf("got %+v, want one delete", actions)
	}
	if actions[0].Existing.Name == "" {
		t.Error("delete action lost the existing resource name")
	}
}

func TestPlanNeverTouchesForeignPoints(t *testing.T) {
	foreign := existingPoint("a1b2c3d4-server-generated", 250)

	actions := Plan(nil, []domain.Point{foreign})

	if len(actions) != 0 {
		t.Fatalf("got %+v, want no actions on foreign points", actions)
	}
}

// mealPointFor constructs a point with a specific meal type (needed now that
// the planner uses (date, meal, type) as a semantic fallback key).
func mealPointFor(id string, meal domain.Meal, kcal float64) domain.Point {
	return domain.Point{
		ID:     id,
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   meal,
		Macros: domain.Macros{EnergyKcal: kcal, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
}

func existingMealPoint(id string, meal domain.Meal, kcal float64) domain.Point {
	p := mealPointFor(id, meal, kcal)
	p.Name = "users/u1/dataTypes/nutrition-log/dataPoints/" + id
	return p
}

func TestPlanMixedDay(t *testing.T) {
	desired := []domain.Point{
		mealPointFor("yazio-2026-06-10-breakfast", domain.Breakfast, 326),
		mealPointFor("yazio-2026-06-10-lunch", domain.Lunch, 520),
		{ID: "yazio-2026-06-10-water", Type: domain.HydrationPoint, Date: "2026-06-10", WaterML: 1650},
	}
	existing := []domain.Point{
		existingMealPoint("yazio-2026-06-10-lunch", domain.Lunch, 408),   // changed -> patch
		existingMealPoint("yazio-2026-06-10-dinner", domain.Dinner, 783), // removed -> delete
		{ID: "yazio-2026-06-10-water", Name: "users/u1/dataTypes/hydration-log/dataPoints/yazio-2026-06-10-water", Type: domain.HydrationPoint, Date: "2026-06-10", WaterML: 1650}, // same -> skip
	}

	actions := Plan(desired, existing)

	got := map[string]Op{}
	for _, a := range actions {
		got[a.pointID()] = a.Op
	}
	want := map[string]Op{
		"yazio-2026-06-10-breakfast": OpCreate,
		"yazio-2026-06-10-lunch":     OpPatch,
		"yazio-2026-06-10-dinner":    OpDelete,
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

func TestPlanSemanticFallbackWhenAPIAssignsServerID(t *testing.T) {
	// The Google Health API may assign a server UUID instead of preserving our
	// client ID. The planner must match by (date, type, meal) so the second
	// run skips instead of creating a duplicate.
	serverIDPoint := domain.Point{
		ID:     "server-uuid-abc",
		Name:   "users/me/dataTypes/nutrition-log/dataPoints/server-uuid-abc",
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   domain.Lunch,
		Macros: domain.Macros{EnergyKcal: 408, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
	desired := []domain.Point{nutritionPoint("yazio-2026-06-10-lunch", 408)}

	actions := Plan(desired, []domain.Point{serverIDPoint})

	if len(actions) != 1 || actions[0].Op != OpSkip {
		t.Fatalf("got %+v, want one skip (semantic fallback matched server-assigned UUID)", actions)
	}
}

func TestPlanSemanticFallbackPatchesChangedServerIDPoint(t *testing.T) {
	serverIDPoint := domain.Point{
		ID:     "server-uuid-abc",
		Name:   "users/me/dataTypes/nutrition-log/dataPoints/server-uuid-abc",
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   domain.Lunch,
		Macros: domain.Macros{EnergyKcal: 300, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
	desired := []domain.Point{nutritionPoint("yazio-2026-06-10-lunch", 408)}

	actions := Plan(desired, []domain.Point{serverIDPoint})

	if len(actions) != 1 || actions[0].Op != OpPatch {
		t.Fatalf("got %+v, want one patch via semantic fallback", actions)
	}
	if actions[0].Existing.Name != serverIDPoint.Name {
		t.Errorf("patch uses wrong name: %s", actions[0].Existing.Name)
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
