package yazio

import (
	"os"
	"testing"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

func TestParseSummary(t *testing.T) {
	data, err := os.ReadFile("testdata/summary_2026-06-10.json")
	if err != nil {
		t.Fatal(err)
	}

	got, err := parseSummary("2026-06-10", data)
	if err != nil {
		t.Fatal(err)
	}

	if got.Date != "2026-06-10" {
		t.Errorf("Date = %q, want 2026-06-10", got.Date)
	}
	if got.WaterML != 1650 {
		t.Errorf("WaterML = %v, want 1650", got.WaterML)
	}

	want := map[domain.Meal]domain.Macros{
		domain.Breakfast: {EnergyKcal: 326.5, CarbsG: 30.450000000000003, ProteinG: 21.7, FatG: 9.9},
		domain.Lunch:     {EnergyKcal: 408, CarbsG: 17, ProteinG: 27, FatG: 27},
		domain.Dinner:    {EnergyKcal: 783, CarbsG: 69, ProteinG: 26, FatG: 38},
		domain.Snack:     {EnergyKcal: 107, CarbsG: 21, ProteinG: 4, FatG: 0},
	}
	for meal, macros := range want {
		if got.Meals[meal] != macros {
			t.Errorf("Meals[%s] = %+v, want %+v", meal, got.Meals[meal], macros)
		}
	}
}

func TestParseSummaryEmptyDay(t *testing.T) {
	got, err := parseSummary("2026-01-01", []byte(`{"water_intake": 0, "meals": {}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.WaterML != 0 {
		t.Errorf("WaterML = %v, want 0", got.WaterML)
	}
	for _, meal := range domain.AllMeals {
		if !got.Meals[meal].IsZero() {
			t.Errorf("Meals[%s] = %+v, want zero", meal, got.Meals[meal])
		}
	}
}

func TestParseSummaryInvalidJSON(t *testing.T) {
	if _, err := parseSummary("2026-01-01", []byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
