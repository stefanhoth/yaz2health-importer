// Package health wraps the Google Health API v4 for reading and writing
// nutrition-log and hydration-log data points.
package health

import (
	"context"
	"fmt"
	"strings"
	"time"

	healthapi "google.golang.org/api/health/v4"
	"google.golang.org/api/option"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

// OAuth scopes for the Google Health API nutrition category. The generated
// client predates the May 2026 write rollout, so the strings are spelled out
// here (see https://developers.google.com/health/scopes).
var Scopes = []string{
	"https://www.googleapis.com/auth/googlehealth.nutrition.readonly",
	"https://www.googleapis.com/auth/googlehealth.nutrition.writeonly",
}

// Sink reads and writes data points for one user.
type Sink struct {
	svc *healthapi.Service
	// user is the {user} resource segment, normally "me".
	user string
	// loc is the timezone used to place meal intervals on the diary date.
	loc *time.Location
}

// New builds a Sink. Pass option.WithTokenSource for real use, or
// option.WithEndpoint plus option.WithoutAuthentication in tests.
func New(ctx context.Context, user string, loc *time.Location, opts ...option.ClientOption) (*Sink, error) {
	svc, err := healthapi.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create health service: %w", err)
	}
	return &Sink{svc: svc, user: user, loc: loc}, nil
}

func (s *Sink) parent(t domain.PointType) string {
	return fmt.Sprintf("users/%s/dataTypes/%s", s.user, t)
}

// List returns all points of one type whose civil start time falls on a
// date between from and to (both inclusive, YYYY-MM-DD).
func (s *Sink) List(ctx context.Context, t domain.PointType, from, to string) ([]domain.Point, error) {
	toEnd, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil, fmt.Errorf("invalid to date %q: %w", to, err)
	}
	// Filter fields use the snake_case form of the data type name.
	field := strings.ReplaceAll(string(t), "-", "_")
	filter := fmt.Sprintf("%s.interval.civil_start_time >= %q AND %s.interval.civil_start_time < %q",
		field, from, field, toEnd.AddDate(0, 0, 1).Format("2006-01-02"))

	var points []domain.Point
	call := s.svc.Users.DataTypes.DataPoints.List(s.parent(t)).Filter(filter)
	err = call.Pages(ctx, func(resp *healthapi.ListDataPointsResponse) error {
		for _, dp := range resp.DataPoints {
			points = append(points, fromDataPoint(dp))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s %s..%s: %w", t, from, to, err)
	}
	return points, nil
}

// Create inserts a new data point, requesting the deterministic client ID in
// the name field. The Google Health API may or may not echo the name back in
// the response (it's a new endpoint); duplicate prevention is handled by the
// semantic fallback in the planner rather than relying on this response.
func (s *Sink) Create(ctx context.Context, p domain.Point) error {
	dp := s.toDataPoint(p)
	dp.Name = s.parent(p.Type) + "/dataPoints/" + p.ID
	if _, err := s.svc.Users.DataTypes.DataPoints.Create(s.parent(p.Type), dp).Context(ctx).Do(); err != nil {
		return fmt.Errorf("create %s: %w", p.ID, err)
	}
	return nil
}

// Patch replaces the values of an existing data point identified by its
// full resource name.
func (s *Sink) Patch(ctx context.Context, name string, p domain.Point) error {
	if _, err := s.svc.Users.DataTypes.DataPoints.Patch(name, s.toDataPoint(p)).Context(ctx).Do(); err != nil {
		return fmt.Errorf("patch %s: %w", name, err)
	}
	return nil
}

// Delete removes data points of one type by their full resource names.
// Points not owned by this OAuth client (DATA_POINT_NOT_OWNED_BY_CLIENT) are
// silently skipped — they belong to another app (e.g. Yazio's own sync).
func (s *Sink) Delete(ctx context.Context, t domain.PointType, names []string) error {
	if len(names) == 0 {
		return nil
	}
	req := &healthapi.BatchDeleteDataPointsRequest{Names: names}
	if _, err := s.svc.Users.DataTypes.DataPoints.BatchDelete(s.parent(t), req).Context(ctx).Do(); err != nil {
		if isNotOwned(err) {
			// Batch failed because at least one point is foreign. Retry one-by-one.
			for _, name := range names {
				single := &healthapi.BatchDeleteDataPointsRequest{Names: []string{name}}
				if _, err2 := s.svc.Users.DataTypes.DataPoints.BatchDelete(s.parent(t), single).Context(ctx).Do(); err2 != nil {
					if isNotOwned(err2) {
						continue // silently skip foreign points
					}
					return fmt.Errorf("delete %s: %w", name, err2)
				}
			}
			return nil
		}
		return fmt.Errorf("batch delete %d %s points: %w", len(names), t, err)
	}
	return nil
}

func isNotOwned(err error) bool {
	return strings.Contains(err.Error(), "DATA_POINT_NOT_OWNED_BY_CLIENT")
}

// Representative local times for each diary section. Yazio only knows the
// day a meal belongs to, not when it was eaten.
var mealClock = map[domain.Meal]int{
	domain.Breakfast: 8,
	domain.Lunch:     13,
	domain.Snack:     16,
	domain.Dinner:    19,
}

const waterClock = 12

var mealTypeFor = map[domain.Meal]string{
	domain.Breakfast: "BREAKFAST",
	domain.Lunch:     "LUNCH",
	domain.Dinner:    "DINNER",
	domain.Snack:     "SNACK",
}

var mealForType = map[string]domain.Meal{
	"BREAKFAST": domain.Breakfast,
	"LUNCH":     domain.Lunch,
	"DINNER":    domain.Dinner,
	"SNACK":     domain.Snack,
}

var mealDisplayName = map[domain.Meal]string{
	domain.Breakfast: "Yazio: Frühstück",
	domain.Lunch:     "Yazio: Mittagessen",
	domain.Dinner:    "Yazio: Abendessen",
	domain.Snack:     "Yazio: Snacks",
}

func (s *Sink) toDataPoint(p domain.Point) *healthapi.DataPoint {
	switch p.Type {
	case domain.HydrationPoint:
		return &healthapi.DataPoint{
			HydrationLog: &healthapi.HydrationLog{
				AmountConsumed: &healthapi.VolumeQuantity{
					Milliliters:      p.WaterML,
					UserProvidedUnit: "MILLILITER",
				},
				Interval: s.interval(p.Date, waterClock),
			},
		}
	default:
		return &healthapi.DataPoint{
			NutritionLog: &healthapi.NutritionLog{
				FoodDisplayName: mealDisplayName[p.Meal],
				MealType:        mealTypeFor[p.Meal],
				Energy: &healthapi.EnergyQuantity{
					Kcal:             p.Macros.EnergyKcal,
					UserProvidedUnit: "KILOCALORIE",
				},
				TotalCarbohydrate: &healthapi.WeightQuantity{Grams: p.Macros.CarbsG},
				TotalFat:          &healthapi.WeightQuantity{Grams: p.Macros.FatG},
				Nutrients: []*healthapi.NutrientQuantity{{
					Nutrient: "PROTEIN",
					Quantity: &healthapi.WeightQuantity{Grams: p.Macros.ProteinG},
				}},
				Interval: s.interval(p.Date, mealClock[p.Meal]),
			},
		}
	}
}

// interval places a 15-minute session at the given local hour of the diary
// date. Falls back to a zero time on a malformed date, which the API will
// reject loudly rather than us guessing.
func (s *Sink) interval(date string, hour int) *healthapi.SessionTimeInterval {
	day, err := time.ParseInLocation("2006-01-02", date, s.loc)
	if err != nil {
		return &healthapi.SessionTimeInterval{}
	}
	start := day.Add(time.Duration(hour) * time.Hour)
	end := start.Add(15 * time.Minute)
	_, startOff := start.Zone()
	_, endOff := end.Zone()
	return &healthapi.SessionTimeInterval{
		StartTime:      start.Format(time.RFC3339),
		EndTime:        end.Format(time.RFC3339),
		StartUtcOffset: fmt.Sprintf("%ds", startOff),
		EndUtcOffset:   fmt.Sprintf("%ds", endOff),
	}
}

func fromDataPoint(dp *healthapi.DataPoint) domain.Point {
	p := domain.Point{Name: dp.Name}
	if idx := strings.LastIndex(dp.Name, "/"); idx >= 0 {
		p.ID = dp.Name[idx+1:]
	}
	switch {
	case dp.HydrationLog != nil:
		p.Type = domain.HydrationPoint
		if dp.HydrationLog.AmountConsumed != nil {
			p.WaterML = dp.HydrationLog.AmountConsumed.Milliliters
		}
		p.Date = intervalDate(dp.HydrationLog.Interval)
	case dp.NutritionLog != nil:
		n := dp.NutritionLog
		p.Type = domain.NutritionPoint
		p.Meal = mealForType[n.MealType]
		if n.Energy != nil {
			p.Macros.EnergyKcal = n.Energy.Kcal
		}
		if n.TotalCarbohydrate != nil {
			p.Macros.CarbsG = n.TotalCarbohydrate.Grams
		}
		if n.TotalFat != nil {
			p.Macros.FatG = n.TotalFat.Grams
		}
		for _, nutrient := range n.Nutrients {
			if nutrient.Nutrient == "PROTEIN" && nutrient.Quantity != nil {
				p.Macros.ProteinG = nutrient.Quantity.Grams
			}
		}
		p.Date = intervalDate(n.Interval)
	}
	return p
}

// intervalDate extracts the civil calendar date a point belongs to.
func intervalDate(iv *healthapi.SessionTimeInterval) string {
	if iv == nil {
		return ""
	}
	if iv.CivilStartTime != nil && iv.CivilStartTime.Date != nil {
		d := iv.CivilStartTime.Date
		return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
	}
	// Fall back to the physical start time shifted by the stored UTC offset.
	start, err := time.Parse(time.RFC3339, iv.StartTime)
	if err != nil {
		return ""
	}
	var offset time.Duration
	if iv.StartUtcOffset != "" {
		if parsed, err := time.ParseDuration(strings.TrimSuffix(iv.StartUtcOffset, "s") + "s"); err == nil {
			offset = parsed
		}
	}
	return start.UTC().Add(offset).Format("2006-01-02")
}
