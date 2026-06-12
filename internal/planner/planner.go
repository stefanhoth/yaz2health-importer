// Package planner computes the write actions needed to make Google Health
// match Yazio. It is a pure diff over normalized points; Google is the
// source of truth for the existing state, so the sync stays stateless and
// idempotent.
package planner

import (
	"sort"

	"github.com/stefanhoth/yaz2health/internal/domain"
)

// Op is the kind of action to take for one data point.
type Op string

const (
	OpCreate Op = "create"
	OpPatch  Op = "patch"
	OpDelete Op = "delete"
	OpSkip   Op = "skip"
)

// Action pairs an operation with the point(s) it applies to.
type Action struct {
	Op Op
	// Desired is set for create and patch.
	Desired domain.Point
	// Existing is set for patch, delete, and skip; carries the resource Name.
	Existing domain.Point
}

// semKey identifies a point by (date, type, meal). Google Health does not
// preserve client-provided IDs, so this is the sole matching key.
type semKey struct {
	date, meal string
	t          domain.PointType
}

func semKeyOf(p domain.Point) semKey {
	return semKey{date: p.Date, meal: string(p.Meal), t: p.Type}
}

// Plan diffs desired against existing points. Both slices must cover the same
// date range. Each desired point is matched against existing by semantic key
// (date + type + meal); unmatched desired points are created, matched points
// are skipped or patched. Existing points with no desired counterpart are
// marked for deletion — the API enforces ownership and will silently reject
// attempts to delete points created by other apps or the user directly.
// Actions are ordered by point ID for stable output.
func Plan(desired, existing []domain.Point) []Action {
	existingBySem := make(map[semKey]domain.Point, len(existing))
	for _, p := range existing {
		existingBySem[semKeyOf(p)] = p
	}

	var actions []Action
	for _, want := range desired {
		have, ok := existingBySem[semKeyOf(want)]
		switch {
		case !ok:
			actions = append(actions, Action{Op: OpCreate, Desired: want})
		case want.SameValues(have):
			actions = append(actions, Action{Op: OpSkip, Desired: want, Existing: have})
		default:
			actions = append(actions, Action{Op: OpPatch, Desired: want, Existing: have})
		}
		delete(existingBySem, semKeyOf(want))
	}

	for _, have := range existingBySem {
		actions = append(actions, Action{Op: OpDelete, Existing: have})
	}

	sort.Slice(actions, func(i, j int) bool {
		return actions[i].pointID() < actions[j].pointID()
	})
	return actions
}

func (a Action) pointID() string {
	if a.Desired.ID != "" {
		return a.Desired.ID
	}
	return a.Existing.ID
}
