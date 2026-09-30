package output

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/go-github/v63/github"
)

// maxRetryAttempts bounds retry-on-rate-limit loops (per method call, e.g. one
// CommitFiles invocation), per the "bounded to a small number of attempts" requirement.
const maxRetryAttempts = 4

// baseBackoff is the starting delay for exponential backoff between retries when we
// don't have a more precise signal (e.g. no Retry-After / rate-limit reset time).
const baseBackoff = 500 * time.Millisecond

// maxBackoff caps how long a single computed wait (including a rate-limit reset wait)
// is allowed to be, so a misbehaving/misreported reset time far in the future can't
// stall a request for an unreasonable duration.
const maxBackoff = 30 * time.Second

// withRateLimitRetry runs fn, retrying with backoff when the error indicates GitHub
// API rate limiting (either go-github's typed *github.RateLimitError /
// *github.AbuseRateLimitError, or a raw 403 response with X-RateLimit-Remaining: 0).
// Any other error is returned immediately without retry. After maxRetryAttempts
// failed-due-to-rate-limit attempts, it returns a *rateLimitedError wrapping
// ErrRateLimited.
func withRateLimitRetry(ctx context.Context, fn func() (*github.Response, error)) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		resp, err := fn()
		if err == nil {
			return nil
		}

		wait, isRateLimit := rateLimitWait(resp, err)
		if !isRateLimit {
			return err
		}

		lastErr = err
		if attempt == maxRetryAttempts {
			break
		}

		if wait <= 0 {
			// Exponential backoff fallback when we have no explicit reset/Retry-After
			// signal: 500ms, 1s, 2s, ...
			wait = baseBackoff * time.Duration(1<<(attempt-1))
		}
		if wait > maxBackoff {
			wait = maxBackoff
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	return &rateLimitedError{attempts: maxRetryAttempts, last: lastErr}
}

// rateLimitWait inspects a go-github error/response and reports whether it represents
// rate limiting, plus how long the caller should wait before retrying (0 means "no
// specific signal, use exponential backoff instead").
func rateLimitWait(resp *github.Response, err error) (wait time.Duration, isRateLimit bool) {
	if err == nil {
		return 0, false
	}

	var rle *github.RateLimitError
	if errors.As(err, &rle) {
		return timeUntil(rle.Rate.Reset.Time), true
	}

	var arle *github.AbuseRateLimitError
	if errors.As(err, &arle) {
		if arle.RetryAfter != nil {
			return *arle.RetryAfter, true
		}
		return 0, true
	}

	// Fall back to raw status/header inspection in case the error wasn't wrapped into
	// one of go-github's typed rate-limit errors (e.g. a secondary rate limit surfaced
	// as a plain *github.ErrorResponse with 403 and the remaining-quota header).
	if resp != nil && resp.Response != nil && resp.StatusCode == http.StatusForbidden {
		if resp.Response.Header.Get("X-RateLimit-Remaining") == "0" {
			if resetHeader := resp.Response.Header.Get("X-RateLimit-Reset"); resetHeader != "" {
				if resetUnix, perr := parseUnixSeconds(resetHeader); perr == nil {
					return timeUntil(time.Unix(resetUnix, 0)), true
				}
			}
			return 0, true
		}
		if retryAfter := resp.Response.Header.Get("Retry-After"); retryAfter != "" {
			if secs, perr := parseUnixSeconds(retryAfter); perr == nil {
				return time.Duration(secs) * time.Second, true
			}
		}
	}

	return 0, false
}

func timeUntil(t time.Time) time.Duration {
	if t.IsZero() {
		return 0
	}
	d := time.Until(t)
	if d < 0 {
		return 0
	}
	return d
}

func parseUnixSeconds(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
