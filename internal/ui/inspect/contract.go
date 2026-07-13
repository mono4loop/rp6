package inspect

import "fmt"

// Contract describes reusable semantic layout assertions for one scene.
type Contract struct {
	Required       []string
	Hidden         []string
	Fit            []string
	NonOverlapping []string
	TouchTargets   []string
	MinTouch       Size
}

// Problem is one failed layout contract assertion.
type Problem struct {
	Code    string `json:"code"`
	ID      string `json:"id,omitempty"`
	OtherID string `json:"otherID,omitempty"`
	Message string `json:"message"`
}

func (p Problem) String() string { return p.Message }

// Check evaluates a captured snapshot against contract.
func Check(snapshot Snapshot, contract Contract) []Problem {
	byID := make(map[string][]Element, len(snapshot.Elements))
	for _, e := range snapshot.Elements {
		byID[e.ID] = append(byID[e.ID], e)
	}
	problems := checkDuplicates(byID)
	problems = append(problems, checkRequired(byID, contract.Required)...)
	problems = append(problems, checkHidden(byID, contract.Hidden)...)
	problems = append(problems, checkFit(byID, contract.Fit)...)
	problems = append(problems, checkOverlaps(byID, contract.NonOverlapping)...)
	problems = append(problems, checkTouchTargets(byID, contract.TouchTargets, contract.MinTouch)...)
	return problems
}

func checkDuplicates(byID map[string][]Element) []Problem {
	var problems []Problem
	for id, elements := range byID {
		if len(elements) > 1 {
			problems = append(problems, problem("duplicate-id", id, "", "semantic ID %q is registered %d times", id, len(elements)))
		}
		for _, e := range elements {
			if e.Duplicate {
				problems = append(problems, problem("duplicate-object", id, "", "semantic object %q occurs %d times in the canvas tree", id, e.Occurrences))
			}
		}
	}
	return problems
}

func checkRequired(byID map[string][]Element, required []string) []Problem {
	var problems []Problem
	for _, id := range required {
		e, ok := one(byID, id)
		switch {
		case !ok:
			problems = append(problems, problem("missing-target", id, "", "required semantic target %q is not registered", id))
		case !e.Present:
			problems = append(problems, problem("not-present", id, "", "required target %q is not in the active canvas tree", id))
		case !e.EffectiveVisible:
			problems = append(problems, problem("not-visible", id, "", "required target %q is not effectively visible", id))
		}
	}
	return problems
}

func checkHidden(byID map[string][]Element, hidden []string) []Problem {
	var problems []Problem
	for _, id := range hidden {
		if e, ok := one(byID, id); ok && e.EffectiveVisible {
			problems = append(problems, problem("unexpected-visible", id, "", "target %q should be left off in this scene", id))
		}
	}
	return problems
}

func checkFit(byID map[string][]Element, fit []string) []Problem {
	var problems []Problem
	for _, id := range fit {
		e, ok := one(byID, id)
		if !ok || !e.Present || !e.SelfVisible {
			continue
		}
		if !e.EffectiveVisible {
			problems = append(problems, problem("offscreen", id, "", "target %q is present and shown but outside its visible clip", id))
			continue
		}
		if e.Clipped {
			problems = append(problems, problem("clipped", id, "", "target %q is clipped: rect %s, visible %s", id, e.Rect, e.VisibleRect))
		}
		if e.UnderMin {
			problems = append(problems, problem("under-min", id, "", "target %q is %.1fx%.1f, below its %.1fx%.1f minimum", id, e.Rect.Width, e.Rect.Height, e.MinSize.Width, e.MinSize.Height))
		}
	}
	return problems
}

func checkOverlaps(byID map[string][]Element, nonOverlapping []string) []Problem {
	var problems []Problem
	for i, id := range nonOverlapping {
		a, ok := one(byID, id)
		if !ok || !a.EffectiveVisible {
			continue
		}
		for _, otherID := range nonOverlapping[i+1:] {
			b, ok := one(byID, otherID)
			if !ok || !b.EffectiveVisible {
				continue
			}
			if overlaps(a.VisibleRect, b.VisibleRect) {
				problems = append(problems, problem("overlap", id, otherID, "targets %q and %q overlap", id, otherID))
			}
		}
	}
	return problems
}

func checkTouchTargets(byID map[string][]Element, touchTargets []string, minTouch Size) []Problem {
	var problems []Problem
	for _, id := range touchTargets {
		e, ok := one(byID, id)
		if !ok || !e.EffectiveVisible {
			continue
		}
		if e.VisibleRect.Width+0.5 < minTouch.Width || e.VisibleRect.Height+0.5 < minTouch.Height {
			problems = append(problems, problem("small-touch-target", id, "", "touch target %q is %.1fx%.1f; need at least %.1fx%.1f", id, e.VisibleRect.Width, e.VisibleRect.Height, minTouch.Width, minTouch.Height))
		}
	}
	return problems
}

func one(byID map[string][]Element, id string) (Element, bool) {
	elements := byID[id]
	// Duplicates are reported separately by checkDuplicates; skipping geometry
	// here avoids choosing an arbitrary occurrence and masking the ownership bug.
	if len(elements) != 1 {
		return Element{}, false
	}
	return elements[0], true
}

func overlaps(a, b Rect) bool {
	const tolerance = 0.5
	return a.X < b.X+b.Width-tolerance && b.X < a.X+a.Width-tolerance &&
		a.Y < b.Y+b.Height-tolerance && b.Y < a.Y+a.Height-tolerance
}

func problem(code, id, otherID, format string, args ...any) Problem {
	return Problem{Code: code, ID: id, OtherID: otherID, Message: fmt.Sprintf(format, args...)}
}
