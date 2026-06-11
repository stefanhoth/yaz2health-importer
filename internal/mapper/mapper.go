// Package mapper turns Yazio day summaries into the desired set of Google
// Health data points.
package mapper

import (
	"fmt"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

// Map produces the desired points for one diary day: one nutrition-log per
// non-empty meal and one hydration-log if any water was tracked. Point IDs
// are deterministic so repeated syncs converge instead of duplicating.
func Map(s domain.DaySummary) []domain.Point {
	var points []domain.Point
	for _, meal := range domain.AllMeals {
		macros := s.Meals[meal]
		if macros.IsZero() {
			continue
		}
		points = append(points, domain.Point{
			ID:     PointID(s.Date, string(meal)),
			Type:   domain.NutritionPoint,
			Date:   s.Date,
			Meal:   meal,
			Macros: macros,
		})
	}
	if s.WaterML > 0 {
		points = append(points, domain.Point{
			ID:      PointID(s.Date, "water"),
			Type:    domain.HydrationPoint,
			Date:    s.Date,
			WaterML: s.WaterML,
		})
	}
	return points
}

// PointID builds the deterministic client-provided data point ID. The API
// requires 4-63 chars of lowercase letters, digits, and hyphens.
func PointID(date, suffix string) string {
	return fmt.Sprintf("%s%s-%s", domain.OwnedIDPrefix, date, suffix)
}
