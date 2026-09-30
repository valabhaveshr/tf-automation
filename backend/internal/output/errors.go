// Package output implements WS-E (Output Repo Integration): committing generated
// Terraform code to the output GitHub repository via the GitHub API, and surfacing
// sync status/conflict information back to the caller (WS-G).
//
// See plan/ARCHITECTURE.md § Phase 1 transaction-flow sequence diagram: this package
// is invoked *after* the DB transaction that persists a record/module_selections has
// committed. The caller (WS-G) is responsible for setting records.sync_status /
// last_commit_sha based on this package's return values — this package never touches
// the database itself.
package output

import (
	"errors"
	"fmt"
)

// ErrOutputModifiedExternally is returned by CommitFiles when the output repo's
// branch (or, in principle, one of the specific files being written) has changed
// since this package last read it — i.e. someone/something modified the output
// repo outside the app. Per plan/ARCHITECTURE.md § Edge cases ("Storage /
// consistency" table): "Contents API's SHA-based conditional update fails; app
// refuses to overwrite and surfaces sync_status=failed with an explicit ... message,
// rather than silently clobbering manual changes."
//
// Callers (WS-G) should check for this with errors.Is and set
// sync_status=failed with a message distinct from a generic/transient failure,
// so a human knows a retry alone won't fix it — the record needs review.
var ErrOutputModifiedExternally = errors.New("output repo was modified outside the app; refusing to overwrite (conditional update conflict)")

// ErrRateLimited is returned when CommitFiles/GetFile exhausted its bounded retry
// budget while being rate-limited by the GitHub API. This is a transient failure —
// unlike ErrOutputModifiedExternally, a later retry (e.g. the reconciliation job for
// sync_status=pending/failed records) may well succeed.
var ErrRateLimited = errors.New("github API rate limit exceeded after retries")

// ErrFileNotFound is returned by GetFile when the requested path does not exist in
// the repo at the given ref.
var ErrFileNotFound = errors.New("file not found in output repo")

// conflictError wraps ErrOutputModifiedExternally with the underlying GitHub API
// detail so logs/errors surfaced to an admin still carry the original message.
type conflictError struct {
	detail string
}

func (e *conflictError) Error() string {
	return fmt.Sprintf("%s: %s", ErrOutputModifiedExternally.Error(), e.detail)
}

func (e *conflictError) Unwrap() error {
	return ErrOutputModifiedExternally
}

func newConflictError(detail string) error {
	return &conflictError{detail: detail}
}

// rateLimitedError wraps ErrRateLimited with the number of attempts made.
type rateLimitedError struct {
	attempts int
	last     error
}

func (e *rateLimitedError) Error() string {
	if e.last != nil {
		return fmt.Sprintf("%s (after %d attempts, last error: %v)", ErrRateLimited.Error(), e.attempts, e.last)
	}
	return fmt.Sprintf("%s (after %d attempts)", ErrRateLimited.Error(), e.attempts)
}

func (e *rateLimitedError) Unwrap() error {
	return ErrRateLimited
}
