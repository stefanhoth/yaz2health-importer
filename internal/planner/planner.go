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

// Plan diffs desired against existing points. Both slices must cover the
// same date range. Existing points not owned by this importer (no "yazio-"
// ID prefix, e.g. logged manually in the Health app) are ignored entirely.
// Actions are ordered by point ID for stable output.
func Plan(desired, existing []domain.Point) []Action {
	existingByID := make(map[string]domain.Point, len(existing))
	for _, p := range existing {
		if p.Owned() {
			existingByID[p.ID] = p
		}
	}

	var actions []Action
	for _, want := range desired {
		have, ok := existingByID[want.ID]
		switch {
		case !ok:
			actions = append(actions, Action{Op: OpCreate, Desired: want})
		case want.SameValues(have):
			actions = append(actions, Action{Op: OpSkip, Desired: want, Existing: have})
		default:
			actions = append(actions, Action{Op: OpPatch, Desired: want, Existing: have})
		}
		delete(existingByID, want.ID)
	}

	// Owned points with no desired counterpart were removed in Yazio.
	for _, have := range existingByID {
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
