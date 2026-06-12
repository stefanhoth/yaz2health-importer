// Package syncer orchestrates one sync run: fetch Yazio days, diff against
// Google Health, apply the resulting actions.
package syncer

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/stefanhoth/yaz2health/internal/domain"
	"github.com/stefanhoth/yaz2health/internal/mapper"
	"github.com/stefanhoth/yaz2health/internal/planner"
)

// Source provides Yazio diary days.
type Source interface {
	DaySummary(ctx context.Context, date string) (domain.DaySummary, error)
}

// Sink reads and writes Google Health data points.
type Sink interface {
	List(ctx context.Context, t domain.PointType, from, to string) ([]domain.Point, error)
	Create(ctx context.Context, p domain.Point) error
	Patch(ctx context.Context, name string, p domain.Point) error
	Delete(ctx context.Context, t domain.PointType, names []string) error
}

// Syncer runs the fetch → map → plan → apply pipeline.
type Syncer struct {
	Source Source
	Sink   Sink
	// DryRun logs planned actions without writing.
	DryRun bool
	// Verbose logs existing points read from Google Health before the diff.
	Verbose bool
	// Throttle is slept between Yazio fetches to respect rate limits on
	// long backfills.
	Throttle time.Duration
	// Out receives the per-action log lines.
	Out io.Writer
}

// Stats counts the actions of one run.
type Stats struct {
	Created, Patched, Deleted, Skipped int
}

func (s Stats) String() string {
	return fmt.Sprintf("created=%d patched=%d deleted=%d skipped=%d", s.Created, s.Patched, s.Deleted, s.Skipped)
}

// Run syncs all days from `from` to `to` (inclusive, YYYY-MM-DD).
func (s *Syncer) Run(ctx context.Context, from, to string) (Stats, error) {
	dates, err := dateRange(from, to)
	if err != nil {
		return Stats{}, err
	}

	var desired []domain.Point
	for i, date := range dates {
		if i > 0 && s.Throttle > 0 {
			time.Sleep(s.Throttle)
		}
		day, err := s.Source.DaySummary(ctx, date)
		if err != nil {
			return Stats{}, fmt.Errorf("fetch yazio %s: %w", date, err)
		}
		desired = append(desired, mapper.Map(day)...)
	}

	var existing []domain.Point
	for _, t := range []domain.PointType{domain.NutritionPoint, domain.HydrationPoint} {
		points, err := s.Sink.List(ctx, t, from, to)
		if err != nil {
			return Stats{}, err
		}
		existing = append(existing, points...)
	}

	if s.Verbose {
		s.logf("existing points from Google Health (%d):", len(existing))
		for _, p := range existing {
			s.logf("  id=%q name=%q type=%s date=%q meal=%q water=%.0f kcal=%.0f",
				p.ID, p.Name, p.Type, p.Date, p.Meal, p.WaterML, p.Macros.EnergyKcal)
		}
	}

	return s.apply(ctx, planner.Plan(desired, existing))
}

func (s *Syncer) apply(ctx context.Context, actions []planner.Action) (Stats, error) {
	var stats Stats
	deletes := map[domain.PointType][]string{}

	for _, action := range actions {
		switch action.Op {
		case planner.OpSkip:
			stats.Skipped++
			continue
		case planner.OpCreate:
			s.logf("%s %s (%s)", action.Op, action.Desired.ID, describe(action.Desired))
			if !s.DryRun {
				if err := s.Sink.Create(ctx, action.Desired); err != nil {
					return stats, err
				}
			}
			stats.Created++
		case planner.OpPatch:
			s.logf("%s %s (%s -> %s)", action.Op, action.Desired.ID, describe(action.Existing), describe(action.Desired))
			if !s.DryRun {
				if err := s.Sink.Patch(ctx, action.Existing.Name, action.Desired); err != nil {
					// Google Health's Patch endpoint returns 500 for some points; fall
					// back to delete + create which is more reliable on this new API.
					s.logf("patch failed (%v), retrying as delete+create", err)
					if err2 := s.Sink.Delete(ctx, action.Existing.Type, []string{action.Existing.Name}); err2 != nil {
						return stats, fmt.Errorf("patch fallback delete: %w", err2)
					}
					if err2 := s.Sink.Create(ctx, action.Desired); err2 != nil {
						return stats, fmt.Errorf("patch fallback create: %w", err2)
					}
				}
			}
			stats.Patched++
		case planner.OpDelete:
			s.logf("%s %s (%s)", action.Op, action.Existing.ID, describe(action.Existing))
			deletes[action.Existing.Type] = append(deletes[action.Existing.Type], action.Existing.Name)
			stats.Deleted++
		}
	}

	if !s.DryRun {
		for t, names := range deletes {
			if err := s.Sink.Delete(ctx, t, names); err != nil {
				return stats, err
			}
		}
	}
	return stats, nil
}

func describe(p domain.Point) string {
	if p.Type == domain.HydrationPoint {
		return fmt.Sprintf("%.0f ml", p.WaterML)
	}
	return fmt.Sprintf("%.0f kcal", p.Macros.EnergyKcal)
}

func (s *Syncer) logf(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format+"\n", args...)
	}
}

func dateRange(from, to string) ([]string, error) {
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return nil, fmt.Errorf("invalid from date %q: %w", from, err)
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil, fmt.Errorf("invalid to date %q: %w", to, err)
	}
	if end.Before(start) {
		return nil, fmt.Errorf("to date %s before from date %s", to, from)
	}
	var dates []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d.Format("2006-01-02"))
	}
	return dates, nil
}
