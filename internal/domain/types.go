// Package domain holds the data structures shared between the Yazio source
// and the Google Health sink.
package domain

// Meal identifies one of Yazio's four diary sections.
type Meal string

const (
	Breakfast Meal = "breakfast"
	Lunch     Meal = "lunch"
	Dinner    Meal = "dinner"
	Snack     Meal = "snack"
)

// AllMeals lists meals in chronological order.
var AllMeals = []Meal{Breakfast, Lunch, Snack, Dinner}

// Macros holds the nutrition values Yazio reports per meal.
type Macros struct {
	EnergyKcal float64
	CarbsG     float64
	ProteinG   float64
	FatG       float64
}

// IsZero reports whether nothing was logged for this meal.
func (m Macros) IsZero() bool {
	return m.EnergyKcal == 0 && m.CarbsG == 0 && m.ProteinG == 0 && m.FatG == 0
}

// DaySummary is one Yazio diary day. Date is the Yazio (UTC-based) calendar
// date in YYYY-MM-DD format.
type DaySummary struct {
	Date    string
	Meals   map[Meal]Macros
	WaterML float64
}
