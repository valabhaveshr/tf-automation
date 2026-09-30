package data

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
)

// AllocateNamingCounter atomically allocates and returns the next
// sequential num_id for the given (businessShortCode, envName,
// resourceType) tuple.
//
// Concurrency: the allocation is a single INSERT ... ON CONFLICT ... DO
// UPDATE ... RETURNING statement. Postgres takes the row's lock for the
// duration of that one statement, so two concurrent callers racing for the
// same tuple are serialized by Postgres itself (the second blocks until
// the first's statement completes, then sees the already-incremented
// value) — no two callers can ever receive the same allocated value, and
// unrelated (business_short_code, env_name, resource_type) tuples are
// completely unaffected by this row-level lock. This matches
// plan/ARCHITECTURE.md's requirement that allocation happen atomically at
// save time via a row-locked increment.
//
// width is the naming template's configured num_id_width (e.g. width=2 ->
// values 1..99 are valid). If the value that would be allocated exceeds
// 10^width - 1, the whole allocation is rolled back (the counter is left
// exactly as it was) and ErrCounterExhausted is returned — so a save that
// gets rejected for width exhaustion never burns a counter value that a
// later template-width increase could otherwise have used, and the row
// stays retry-able forever once the config is fixed.
func (s *Store) AllocateNamingCounter(ctx context.Context, businessShortCode, envName, resourceType string, width int) (int, error) {
	if width <= 0 {
		return 0, fmt.Errorf("data: allocate naming counter: width must be positive, got %d", width)
	}
	maxValue := int(math.Pow10(width)) - 1

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("data: allocate naming counter: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed; this is how we "undo" an exhausted allocation

	const q = `
		INSERT INTO naming_counters (business_short_code, env_name, resource_type, next_value)
		VALUES ($1, $2, $3, 2)
		ON CONFLICT (business_short_code, env_name, resource_type) DO UPDATE
			SET next_value = naming_counters.next_value + 1
		RETURNING next_value - 1`

	var allocated int
	if err := tx.QueryRow(ctx, q, businessShortCode, envName, resourceType).Scan(&allocated); err != nil {
		return 0, fmt.Errorf("data: allocate naming counter: %w", err)
	}

	if allocated > maxValue {
		// Returning here without committing rolls back the increment above
		// (see deferred tx.Rollback), so this tuple's counter is unaffected
		// by the rejected attempt.
		return 0, fmt.Errorf("%w: (business_short_code=%s, env=%s, resource_type=%s) at width %d (max %d)",
			ErrCounterExhausted, businessShortCode, envName, resourceType, width, maxValue)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("data: allocate naming counter: commit: %w", err)
	}
	return allocated, nil
}

// PeekNamingCounter returns the value that WOULD be allocated next for the
// given (businessShortCode, envName, resourceType) tuple, without
// incrementing or locking the counter row. This is a read-only preview
// used by the Naming Convention Engine's (WS-I) Suggest path — allocation
// happens only at save time via AllocateNamingCounter, per
// plan/ARCHITECTURE.md's locked decision ("allocation happens atomically
// at save time, not preview").
//
// If no counter row exists yet for this tuple, the tentative next value is
// 1 (matching AllocateNamingCounter's first-insert behavior, which starts
// next_value at 2 and RETURNs next_value - 1 = 1).
//
// Because this performs no locking, the returned value is only a
// tentative preview: a concurrent (or later) AllocateNamingCounter call
// can consume it before/instead of the previewing caller. Callers must
// never treat this as reserved.
func (s *Store) PeekNamingCounter(ctx context.Context, businessShortCode, envName, resourceType string) (int, error) {
	const q = `SELECT next_value FROM naming_counters WHERE business_short_code = $1 AND env_name = $2 AND resource_type = $3`
	var nextValue int
	err := s.pool.QueryRow(ctx, q, businessShortCode, envName, resourceType).Scan(&nextValue)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 1, nil
		}
		return 0, fmt.Errorf("data: peek naming counter: %w", err)
	}
	return nextValue, nil
}
