// Package yazio reads diary data by shelling out to the yazio CLI
// (https://yzapi.yazio.com reverse-engineered client). The CLI owns the
// auth token lifecycle, so this package stays stateless.
package yazio

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

// Client fetches day summaries via the yazio binary.
type Client struct {
	// Binary is the yazio executable name or path. Defaults to "yazio".
	Binary string
}

// summaryJSON mirrors the subset of `yazio --output json summary` we need.
type summaryJSON struct {
	WaterIntake float64             `json:"water_intake"`
	Meals       map[string]mealJSON `json:"meals"`
}

type mealJSON struct {
	Nutrients map[string]float64 `json:"nutrients"`
}

// DaySummary runs `yazio --output json summary <date>` and parses the result.
// Date must be YYYY-MM-DD (Yazio days are UTC-based).
func (c *Client) DaySummary(ctx context.Context, date string) (domain.DaySummary, error) {
	binary := c.Binary
	if binary == "" {
		binary = "yazio"
	}
	out, err := exec.CommandContext(ctx, binary, "--output", "json", "summary", date).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return domain.DaySummary{}, fmt.Errorf("yazio summary %s: %w: %s", date, err, exitErr.Stderr)
		}
		return domain.DaySummary{}, fmt.Errorf("yazio summary %s: %w", date, err)
	}
	return parseSummary(date, out)
}

func parseSummary(date string, data []byte) (domain.DaySummary, error) {
	var raw summaryJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return domain.DaySummary{}, fmt.Errorf("parse yazio summary %s: %w", date, err)
	}
	summary := domain.DaySummary{
		Date:    date,
		Meals:   make(map[domain.Meal]domain.Macros, len(domain.AllMeals)),
		WaterML: raw.WaterIntake,
	}
	for _, meal := range domain.AllMeals {
		nutrients := raw.Meals[string(meal)].Nutrients
		summary.Meals[meal] = domain.Macros{
			EnergyKcal: nutrients["energy.energy"],
			CarbsG:     nutrients["nutrient.carb"],
			ProteinG:   nutrients["nutrient.protein"],
			FatG:       nutrients["nutrient.fat"],
		}
	}
	return summary, nil
}
