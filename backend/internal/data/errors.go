package data

import (
	"errors"
	"fmt"
)

// Sentinel error categories. The API layer (WS-G) maps these to HTTP status
// codes via errors.Is (or a type-switch on ConflictError, which wraps
// ErrConflict and carries the offending constraint name for a friendly
// message). Do not compare returned errors with == directly; always use
// errors.Is/errors.As, since concrete errors returned by this package wrap
// these sentinels with additional context.
var (
	// ErrNotFound is returned when a lookup by ID (or other unique key) finds
	// no matching, non-deleted row. Maps to HTTP 404.
	ErrNotFound = errors.New("data: not found")

	// ErrStaleWrite is returned by UpdateRecord when the optimistic-
	// concurrency check (WHERE id = $1 AND updated_at = $2) affects zero rows
	// because the caller's expected updated_at no longer matches the current
	// row. Maps to HTTP 409 (If-Match mismatch).
	ErrStaleWrite = errors.New("data: stale write: record was modified since it was last read")

	// ErrConflict is returned (usually wrapped in *ConflictError) when a
	// write violates a UNIQUE constraint, e.g. (customer_name, project_name,
	// env_name) or (module_name, version_tag). Maps to HTTP 409.
	ErrConflict = errors.New("data: conflict: unique constraint violated")

	// ErrCounterExhausted is returned by AllocateNamingCounter when the next
	// value for a (business_short_code, env_name, resource_type) tuple would
	// exceed 10^width - 1. Maps to HTTP 400/409 (save rejected, naming the
	// exhausted counter) per plan/ARCHITECTURE.md's naming-convention edge
	// cases.
	ErrCounterExhausted = errors.New("data: naming counter exhausted for configured width")
)

// ConflictError wraps ErrConflict with the specific Postgres constraint name
// that was violated, so the API layer can translate it into a friendly,
// field-specific message (e.g. "this env already exists, did you mean to
// edit it?") instead of a raw 500/generic 409.
type ConflictError struct {
	Constraint string
	Detail     string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("data: conflict: constraint %q violated: %s", e.Constraint, e.Detail)
}

// Unwrap allows errors.Is(err, ErrConflict) to succeed for a *ConflictError.
func (e *ConflictError) Unwrap() error { return ErrConflict }
