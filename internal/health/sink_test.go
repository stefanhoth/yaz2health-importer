package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/api/option"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// newTestSink wires a Sink to an httptest server.
func newTestSink(t *testing.T, handler http.Handler) *Sink {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	sink, err := New(context.Background(), "me", berlin(t),
		option.WithEndpoint(server.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return sink
}

func TestCreateSendsClientIDAndPayload(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	sink := newTestSink(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		// Echo the client-provided name back, as the real API should.
		json.NewEncoder(w).Encode(map[string]any{"name": gotBody["name"]})
	}))

	point := domain.Point{
		ID:     "yazio-2026-06-10-lunch",
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   domain.Lunch,
		Macros: domain.Macros{EnergyKcal: 408, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
	if err := sink.Create(context.Background(), point); err != nil {
		t.Fatal(err)
	}

	if gotPath != "/v4/users/me/dataTypes/nutrition-log/dataPoints" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["name"] != "users/me/dataTypes/nutrition-log/dataPoints/yazio-2026-06-10-lunch" {
		t.Errorf("body name = %v", gotBody["name"])
	}

	log := gotBody["nutritionLog"].(map[string]any)
	if log["mealType"] != "LUNCH" {
		t.Errorf("mealType = %v", log["mealType"])
	}
	if kcal := log["energy"].(map[string]any)["kcal"]; kcal != 408.0 {
		t.Errorf("kcal = %v", kcal)
	}
	if carbs := log["totalCarbohydrate"].(map[string]any)["grams"]; carbs != 17.0 {
		t.Errorf("carbs = %v", carbs)
	}
	interval := log["interval"].(map[string]any)
	// June -> CEST, UTC+2: lunch at 13:00 local.
	if interval["startTime"] != "2026-06-10T13:00:00+02:00" {
		t.Errorf("startTime = %v", interval["startTime"])
	}
	if interval["startUtcOffset"] != "7200s" {
		t.Errorf("startUtcOffset = %v", interval["startUtcOffset"])
	}
}

func TestCreateSucceedsWhenResponseNameEmptyOrDiffers(t *testing.T) {
	// The Google Health API may return an empty name in the Create response
	// (observed in the wild). Duplicate prevention is handled by the planner's
	// semantic fallback, not by this response check.
	for _, respName := range []string{"", "users/me/dataTypes/hydration-log/dataPoints/server-uuid"} {
		sink := newTestSink(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"name": respName})
		}))
		err := sink.Create(context.Background(), domain.Point{
			ID:      "yazio-2026-06-10-water",
			Type:    domain.HydrationPoint,
			Date:    "2026-06-10",
			WaterML: 1650,
		})
		if err != nil {
			t.Errorf("respName=%q: unexpected error: %v", respName, err)
		}
	}
}

func TestListFiltersAndNormalizes(t *testing.T) {
	var gotFilter string
	sink := newTestSink(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		json.NewEncoder(w).Encode(map[string]any{
			"dataPoints": []map[string]any{
				{
					"name": "users/u1/dataTypes/nutrition-log/dataPoints/yazio-2026-06-10-lunch",
					"nutritionLog": map[string]any{
						"mealType":          "LUNCH",
						"energy":            map[string]any{"kcal": 408},
						"totalCarbohydrate": map[string]any{"grams": 17},
						"totalFat":          map[string]any{"grams": 27},
						"nutrients": []map[string]any{
							{"nutrient": "PROTEIN", "quantity": map[string]any{"grams": 27}},
						},
						"interval": map[string]any{
							"startTime":      "2026-06-10T11:00:00Z",
							"startUtcOffset": "7200s",
							"civilStartTime": map[string]any{
								"date": map[string]any{"year": 2026, "month": 6, "day": 10},
							},
						},
					},
				},
			},
		})
	}))

	points, err := sink.List(context.Background(), domain.NutritionPoint, "2026-06-08", "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}

	wantFilter := `nutrition_log.interval.civil_start_time >= "2026-06-08" AND nutrition_log.interval.civil_start_time < "2026-06-11"`
	if gotFilter != wantFilter {
		t.Errorf("filter = %q, want %q", gotFilter, wantFilter)
	}
	if len(points) != 1 {
		t.Fatalf("got %d points, want 1", len(points))
	}
	want := domain.Point{
		ID:     "yazio-2026-06-10-lunch",
		Name:   "users/u1/dataTypes/nutrition-log/dataPoints/yazio-2026-06-10-lunch",
		Type:   domain.NutritionPoint,
		Date:   "2026-06-10",
		Meal:   domain.Lunch,
		Macros: domain.Macros{EnergyKcal: 408, CarbsG: 17, ProteinG: 27, FatG: 27},
	}
	if points[0] != want {
		t.Errorf("point = %+v, want %+v", points[0], want)
	}
}

func TestListDateFallbackFromPhysicalTime(t *testing.T) {
	// 23:30 Berlin on June 10 is 21:30 UTC; without civilStartTime the date
	// must still resolve to June 10 via the stored UTC offset.
	sink := newTestSink(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"dataPoints": []map[string]any{
				{
					"name": "users/u1/dataTypes/hydration-log/dataPoints/yazio-2026-06-10-water",
					"hydrationLog": map[string]any{
						"amountConsumed": map[string]any{"milliliters": 1650},
						"interval": map[string]any{
							"startTime":      "2026-06-10T21:30:00Z",
							"startUtcOffset": "7200s",
						},
					},
				},
			},
		})
	}))

	points, err := sink.List(context.Background(), domain.HydrationPoint, "2026-06-10", "2026-06-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Date != "2026-06-10" {
		t.Fatalf("points = %+v, want one with date 2026-06-10", points)
	}
	if points[0].WaterML != 1650 {
		t.Errorf("WaterML = %v, want 1650", points[0].WaterML)
	}
}

func TestDeleteBatchesNames(t *testing.T) {
	var gotPath string
	var gotBody map[string][]string
	sink := newTestSink(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(w).Encode(map[string]any{})
	}))

	names := []string{"users/me/dataTypes/nutrition-log/dataPoints/yazio-2026-06-10-snack"}
	if err := sink.Delete(context.Background(), domain.NutritionPoint, names); err != nil {
		t.Fatal(err)
	}

	if gotPath != "/v4/users/me/dataTypes/nutrition-log/dataPoints:batchDelete" {
		t.Errorf("path = %q", gotPath)
	}
	if len(gotBody["names"]) != 1 || gotBody["names"][0] != names[0] {
		t.Errorf("names = %v", gotBody["names"])
	}
}

func TestDeleteNoopOnEmpty(t *testing.T) {
	sink := newTestSink(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call for empty delete")
	}))
	if err := sink.Delete(context.Background(), domain.NutritionPoint, nil); err != nil {
		t.Fatal(err)
	}
}
