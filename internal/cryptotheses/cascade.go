// Package cryptotheses — cascade engine.
//
// Step 5 implementation of Spec 9l cascade machinery (D13, D18, D19, D20).
// Builds on the crypto_thesis_dependencies + cascade_events tables from
// migration 0031.
//
// Design (per Spec 9l v0.2 §E + handoff doc):
//   - **Async, single-pass, no recursion**. When a thesis rescore lands,
//     CheckCascadeOnRescore walks one level of dependencies and fires
//     events. It does NOT then re-trigger from the affected children.
//   - **Recursion detection at dependency creation** (CreateDependency).
//     BFS from prospective child up the parent chain; reject if the
//     proposed parent is reachable. Max depth 5 (defensive).
//   - **All cascade triggers logged** to cascade_events even when the
//     action is notification-only.
//   - **Cascade trigger thresholds** depend on dependency_type + band drop:
//       platform_parent (strong)      → any band drop  → flagged_needs_review HIGH
//       protocol_host (moderate)      → 2+ band drop   → flagged_needs_review MEDIUM
//       oracle_dependency (moderate)  → 2+ band drop   → flagged_needs_review MEDIUM
//                                    or any drop into Trim/Exit → flagged_needs_review MEDIUM
//       narrative_correlated (any)   → any band drop  → notification_only LOW
//       btc_beta_implicit            → BTC enters Trim/Exit → notification_only LOW
//
// The engine is intentionally adapter-agnostic — it walks the dependency
// graph and applies cascade_strength + dependency_type rules. Adapter-
// specific auto-creation hooks live in the thesis lock flow (D12).

package cryptotheses

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CascadeAction is the audit verb recorded on cascade_events.
type CascadeAction string

const (
	CascadeFlaggedNeedsReview CascadeAction = "flagged_needs_review"
	CascadeNotificationOnly   CascadeAction = "notification_only"
)

// CascadePriority is the ranked urgency on cascade_events.
type CascadePriority string

const (
	PriorityHigh   CascadePriority = "high"
	PriorityMedium CascadePriority = "medium"
	PriorityLow    CascadePriority = "low"
)

// Dependency is one row of crypto_thesis_dependencies.
type Dependency struct {
	ID              int64           `json:"id"`
	ParentThesisID  int64           `json:"parentThesisID"`
	ChildThesisID   int64           `json:"childThesisID"`
	DependencyType  DependencyType  `json:"dependencyType"`
	CascadeStrength CascadeStrength `json:"cascadeStrength"`
	Note            string          `json:"note,omitempty"`
	CreatedBy       string          `json:"createdBy"`
	CreatedAt       int64           `json:"createdAt"`
}

// CascadeEvent is one row of cascade_events.
type CascadeEvent struct {
	ID                   int64           `json:"id"`
	TriggeringThesisID   int64           `json:"triggeringThesisID"`
	AffectedThesisID     int64           `json:"affectedThesisID"`
	DependencyType       DependencyType  `json:"dependencyType"`
	TriggerReason        string          `json:"triggerReason"`
	Action               CascadeAction   `json:"action"`
	Priority             CascadePriority `json:"priority"`
	ResolvedAt           *int64          `json:"resolvedAt,omitempty"`
	ResolutionNote       string          `json:"resolutionNote,omitempty"`
	CreatedAt            int64           `json:"createdAt"`
}

// Errors specific to the cascade engine.
var (
	ErrCascadeCircularDep = errors.New("cascade: would create circular dependency")
	ErrCascadeSelfDep     = errors.New("cascade: parent and child are the same thesis")
	ErrThesisNotFound     = errors.New("cascade: thesis not found")
)

// CascadeService wraps DB ops for the cascade engine.
type CascadeService struct {
	DB *sql.DB
}

// NewCascade returns a service tied to the given DB handle.
func NewCascade(db *sql.DB) *CascadeService { return &CascadeService{DB: db} }

// ----- Dependency CRUD --------------------------------------------------

// CreateDependency inserts a new dependency row after recursion detection.
// Rejects if (a) parent == child or (b) inserting would create a cycle in
// the parent → child graph.
func (s *CascadeService) CreateDependency(ctx context.Context, d Dependency) (int64, error) {
	if d.ParentThesisID == d.ChildThesisID {
		return 0, ErrCascadeSelfDep
	}
	if !d.DependencyType.Valid() {
		return 0, fmt.Errorf("invalid dependency_type: %q", d.DependencyType)
	}
	if !d.CascadeStrength.Valid() {
		return 0, fmt.Errorf("invalid cascade_strength: %q", d.CascadeStrength)
	}
	// Recursion check: if the proposed parent is reachable from the child
	// in the existing graph, inserting would close a cycle.
	if reachable, err := s.parentReachableFromChild(ctx, d.ChildThesisID, d.ParentThesisID, 5); err != nil {
		return 0, fmt.Errorf("recursion check: %w", err)
	} else if reachable {
		return 0, ErrCascadeCircularDep
	}
	if d.CreatedBy == "" {
		d.CreatedBy = "user"
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO crypto_thesis_dependencies
		  (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
		d.ParentThesisID, d.ChildThesisID, d.DependencyType, d.CascadeStrength,
		nullableString(d.Note), d.CreatedBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// parentReachableFromChild walks up the parent chain from `start` looking
// for `target` within `maxDepth` levels. Used by recursion detection.
func (s *CascadeService) parentReachableFromChild(ctx context.Context, start, target int64, maxDepth int) (bool, error) {
	if start == target {
		return true, nil
	}
	queue := []int64{start}
	seen := map[int64]bool{start: true}
	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		next := []int64{}
		for _, id := range queue {
			rows, err := s.DB.QueryContext(ctx,
				`SELECT child_thesis_id FROM crypto_thesis_dependencies WHERE parent_thesis_id = ?`, id)
			if err != nil {
				return false, err
			}
			for rows.Next() {
				var childID int64
				if err := rows.Scan(&childID); err != nil {
					rows.Close()
					return false, err
				}
				if childID == target {
					rows.Close()
					return true, nil
				}
				if !seen[childID] {
					seen[childID] = true
					next = append(next, childID)
				}
			}
			rows.Close()
		}
		queue = next
	}
	return false, nil
}

// ListDependencies returns all dependency rows for a thesis (both
// directions: where it's a parent OR a child).
func (s *CascadeService) ListDependencies(ctx context.Context, thesisID int64) ([]Dependency, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, parent_thesis_id, child_thesis_id, dependency_type, cascade_strength,
		       COALESCE(note,''), created_by, created_at
		  FROM crypto_thesis_dependencies
		 WHERE parent_thesis_id = ? OR child_thesis_id = ?
		 ORDER BY id`, thesisID, thesisID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dependency{}
	for rows.Next() {
		var d Dependency
		if err := rows.Scan(&d.ID, &d.ParentThesisID, &d.ChildThesisID,
			&d.DependencyType, &d.CascadeStrength, &d.Note, &d.CreatedBy, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteDependency removes a single dependency row by id.
func (s *CascadeService) DeleteDependency(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM crypto_thesis_dependencies WHERE id = ?`, id)
	return err
}

// ----- Cascade trigger logic --------------------------------------------

// CheckCascadeOnRescore is the entry point called after a thesis is
// re-scored. Walks one level of children, applies trigger rules per
// dependency_type + cascade_strength + band drop, fires cascade_events
// + updates affected theses to needs-review when warranted.
//
// Single-pass: does NOT recurse into the grandchildren. If you want
// multi-level cascade, call CheckCascadeOnRescore separately for each
// affected thesis after its own rescore.
//
// Returns the events that fired (for logging / UI surface / tests).
func (s *CascadeService) CheckCascadeOnRescore(ctx context.Context, triggeringThesisID int64, oldBand, newBand Band) ([]CascadeEvent, error) {
	if oldBand == newBand {
		return nil, nil // no band change, nothing to cascade
	}
	dropped := BandDropped(oldBand, newBand)
	if !dropped {
		return nil, nil // upgrade — no cascade in v1
	}
	bandDelta := newBand.Rank() - oldBand.Rank()

	// Find all children of the triggering thesis.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, child_thesis_id, dependency_type, cascade_strength
		  FROM crypto_thesis_dependencies
		 WHERE parent_thesis_id = ?`, triggeringThesisID)
	if err != nil {
		return nil, fmt.Errorf("list children: %w", err)
	}
	defer rows.Close()

	type childDep struct {
		depID      int64
		childID    int64
		depType    DependencyType
		strength   CascadeStrength
	}
	var children []childDep
	for rows.Next() {
		var c childDep
		if err := rows.Scan(&c.depID, &c.childID, &c.depType, &c.strength); err != nil {
			return nil, err
		}
		children = append(children, c)
	}
	rows.Close()

	events := make([]CascadeEvent, 0, len(children))
	for _, c := range children {
		fire, action, priority := evaluateTrigger(c.depType, c.strength, oldBand, newBand, bandDelta)
		if !fire {
			continue
		}
		reason := fmt.Sprintf("parent_band_%s_to_%s", oldBand, newBand)

		// Insert cascade_events row.
		res, err := s.DB.ExecContext(ctx, `
			INSERT INTO cascade_events
			  (triggering_thesis_id, affected_thesis_id, dependency_type, trigger_reason, action, priority)
			VALUES (?, ?, ?, ?, ?, ?)`,
			triggeringThesisID, c.childID, c.depType, reason, action, priority)
		if err != nil {
			return events, fmt.Errorf("insert cascade_event for child %d: %w", c.childID, err)
		}
		eventID, _ := res.LastInsertId()

		// If action is flagged_needs_review, update child thesis status.
		if action == CascadeFlaggedNeedsReview {
			if _, err := s.DB.ExecContext(ctx, `
				UPDATE crypto_theses
				   SET status = 'needs-review',
				       updated_at = strftime('%s','now')
				 WHERE id = ? AND status = 'locked'`, c.childID); err != nil {
				return events, fmt.Errorf("flag child %d: %w", c.childID, err)
			}
		}

		events = append(events, CascadeEvent{
			ID:                 eventID,
			TriggeringThesisID: triggeringThesisID,
			AffectedThesisID:   c.childID,
			DependencyType:     c.depType,
			TriggerReason:      reason,
			Action:             action,
			Priority:           priority,
		})
	}
	return events, nil
}

// evaluateTrigger applies the dependency-type-specific rules per Spec 9l
// v0.2 §E "Trigger rules" table.
//
// Returns (fire, action, priority).
func evaluateTrigger(depType DependencyType, strength CascadeStrength, oldBand, newBand Band, delta int) (bool, CascadeAction, CascadePriority) {
	switch depType {
	case DepPlatformParent:
		// Strong: any band drop on parent → flag child needs-review HIGH.
		return true, CascadeFlaggedNeedsReview, PriorityHigh
	case DepProtocolHost:
		// Moderate: 2+ band drop → flag MEDIUM.
		if delta >= 2 {
			return true, CascadeFlaggedNeedsReview, PriorityMedium
		}
		return false, "", ""
	case DepOracleDependency:
		// Moderate: 2+ band drop → flag MEDIUM, OR parent entering Trim/Exit at all → flag MEDIUM.
		if delta >= 2 || newBand == BandTrim || newBand == BandExit {
			return true, CascadeFlaggedNeedsReview, PriorityMedium
		}
		return false, "", ""
	case DepNarrativeCorrelated:
		// Any band drop → notification only LOW.
		return true, CascadeNotificationOnly, PriorityLow
	case DepBTCBetaImplicit:
		// BTC entering Trim/Exit only → notification LOW (no auto-flag).
		if newBand == BandTrim || newBand == BandExit {
			return true, CascadeNotificationOnly, PriorityLow
		}
		return false, "", ""
	}
	return false, "", ""
}

// ----- Cascade event queries --------------------------------------------

// ListCascadeEvents returns events affecting a given thesis (most recent first).
func (s *CascadeService) ListCascadeEvents(ctx context.Context, thesisID int64, unresolvedOnly bool) ([]CascadeEvent, error) {
	q := `
		SELECT id, triggering_thesis_id, affected_thesis_id, dependency_type, trigger_reason,
		       action, priority, resolved_at, COALESCE(resolution_note,''), created_at
		  FROM cascade_events
		 WHERE affected_thesis_id = ?`
	if unresolvedOnly {
		q += ` AND resolved_at IS NULL`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.DB.QueryContext(ctx, q, thesisID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CascadeEvent{}
	for rows.Next() {
		var e CascadeEvent
		var resolvedAt sql.NullInt64
		if err := rows.Scan(&e.ID, &e.TriggeringThesisID, &e.AffectedThesisID,
			&e.DependencyType, &e.TriggerReason, &e.Action, &e.Priority,
			&resolvedAt, &e.ResolutionNote, &e.CreatedAt); err != nil {
			return nil, err
		}
		if resolvedAt.Valid {
			v := resolvedAt.Int64
			e.ResolvedAt = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ResolveCascadeEvent marks an event as resolved with an optional note.
func (s *CascadeService) ResolveCascadeEvent(ctx context.Context, eventID int64, note string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE cascade_events
		   SET resolved_at = strftime('%s','now'),
		       resolution_note = ?
		 WHERE id = ? AND resolved_at IS NULL`,
		nullableString(note), eventID)
	return err
}
