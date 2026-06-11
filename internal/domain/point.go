package domain

import "strings"

// PointType is the Google Health data type a Point belongs to.
type PointType string

const (
	NutritionPoint PointType = "nutrition-log"
	HydrationPoint PointType = "hydration-log"
)

// OwnedIDPrefix marks data points managed by this importer. Points without
// it are never patched or deleted.
const OwnedIDPrefix = "yazio-"

// Point is the normalized form of one Google Health data point, used both
// for the desired state (mapped from Yazio) and the existing state (read
// back from Google). Comparing two Points ignores Name and interval times;
// only the logged values matter.
type Point struct {
	// ID is the deterministic client-provided data point ID,
	// e.g. "yazio-2026-06-11-lunch" or "yazio-2026-06-11-water".
	ID string
	// Name is the full resource name. Only set on points read from Google.
	Name string
	Type PointType
	// Date is the Yazio diary date (YYYY-MM-DD).
	Date string
	// Meal is set for nutrition points only.
	Meal    Meal
	Macros  Macros
	WaterML float64
}

// Owned reports whether this importer manages the point.
func (p Point) Owned() bool {
	return strings.HasPrefix(p.ID, OwnedIDPrefix)
}

// valueTolerance absorbs float rounding between what we write and what the
// API returns.
const valueTolerance = 0.01

// SameValues reports whether two points carry the same logged values.
func (p Point) SameValues(other Point) bool {
	return eq(p.Macros.EnergyKcal, other.Macros.EnergyKcal) &&
		eq(p.Macros.CarbsG, other.Macros.CarbsG) &&
		eq(p.Macros.ProteinG, other.Macros.ProteinG) &&
		eq(p.Macros.FatG, other.Macros.FatG) &&
		eq(p.WaterML, other.WaterML)
}

func eq(a, b float64) bool {
	diff := a - b
	return diff < valueTolerance && diff > -valueTolerance
}
