package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"tf-automation/backend/internal/data"
	"tf-automation/backend/internal/naming"
	"tf-automation/backend/internal/output"
)

// apiError is a handler-constructed error carrying the exact HTTP status
// and message to surface to the caller. Handlers that already know the
// precise response (e.g. "customer needs a business short code") return one
// of these; writeErr recognizes it before falling back to the generic
// sentinel-based mapping below.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return e.message }

func newAPIError(status int, message string) error {
	return &apiError{status: status, message: message}
}

func badRequest(msg string) error  { return newAPIError(http.StatusBadRequest, msg) }
func notFound(msg string) error    { return newAPIError(http.StatusNotFound, msg) }
func conflictErr(msg string) error { return newAPIError(http.StatusConflict, msg) }
func forbidden(msg string) error   { return newAPIError(http.StatusForbidden, msg) }

// writeErr maps err to an HTTP status + JSON {"error": "..."} body, per the
// error-handling contract documented across plan/ARCHITECTURE.md's edge
// case tables and this task's mapping requirements:
//
//   - *apiError (handler-constructed)         -> its own status
//   - data.ErrNotFound                        -> 404
//   - data.ErrStaleWrite                      -> 409
//   - data.ErrConflict / *data.ConflictError   -> 409, friendly message
//   - data.ErrCounterExhausted                -> 400, naming the exhausted counter
//   - naming.ErrCustomerShortCodeMissing       -> 400, specific message
//   - naming.ErrNoTemplateConfigured           -> NEVER reaches here: callers
//     must catch this themselves and downgrade it to a response warning
//     (see records.go's applyNaming), per the locked "must not block the
//     save" rule. If it ever does reach here (a bug), it's treated as a
//     generic 500 rather than silently 4xx'd, so the bug is visible.
//   - output.ErrOutputModifiedExternally       -> NEVER reaches here either:
//     callers must catch this at the commit step and set
//     sync_status=failed rather than fail the HTTP response (the record
//     still saved to the DB per the two-phase DB-then-git model).
//   - anything else                            -> 500
func writeErr(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}

	var apiErr *apiError
	if errors.As(err, &apiErr) {
		writeJSONError(w, apiErr.status, apiErr.message)
		return
	}

	if errors.Is(err, data.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, data.ErrStaleWrite) {
		writeJSONError(w, http.StatusConflict, "record was modified since it was last read (stale If-Match)")
		return
	}
	var conflict *data.ConflictError
	if errors.As(err, &conflict) {
		writeJSONError(w, http.StatusConflict, friendlyConflictMessage(conflict))
		return
	}
	if errors.Is(err, data.ErrConflict) {
		writeJSONError(w, http.StatusConflict, "this already exists, did you mean to edit it?")
		return
	}
	if errors.Is(err, data.ErrCounterExhausted) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, naming.ErrCustomerShortCodeMissing) {
		writeJSONError(w, http.StatusBadRequest, "this customer needs a business short code configured (see /api/admin/customers)")
		return
	}

	// Codegen validation errors ARE the authoritative validation per
	// plan/ARCHITECTURE.md ("server re-validates authoritatively
	// regardless" / "server-side ... checking is the authoritative
	// check") -- map any error surfaced directly from that package to 400
	// with the underlying message, never re-implementing the validation
	// here.
	if isCodegenError(err) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("api: internal error: %v", err)
	writeJSONError(w, http.StatusInternalServerError, "internal error")
}

// isCodegenError is a best-effort heuristic: codegen.RenderRootFiles returns
// plain fmt.Errorf-wrapped errors (no exported sentinel), all prefixed
// "codegen: ". Handlers that call it should generally wrap/tag the error
// themselves (see records.go's renderFiles), but this catches any that
// aren't, so a validation failure never accidentally surfaces as a 500.
func isCodegenError(err error) bool {
	var ve *validationError
	return errors.As(err, &ve)
}

// validationError explicitly tags an error as a 400-worthy validation
// failure (codegen errors, request-shape errors, etc.), so writeErr never
// has to guess by string-matching.
type validationError struct{ err error }

func (v *validationError) Error() string { return v.err.Error() }
func (v *validationError) Unwrap() error { return v.err }

func asValidation(err error) error {
	if err == nil {
		return nil
	}
	return &validationError{err: err}
}

func friendlyConflictMessage(c *data.ConflictError) string {
	switch c.Constraint {
	case "records_customer_name_project_name_env_name_key":
		return "this customer/project/env already exists, did you mean to edit it?"
	case "customers_name_key":
		return "a customer with this name already exists"
	case "customers_business_short_code_key":
		return "this customer short name is already taken; choose another"
	case "users_username_key":
		return "this username is already taken"
	case "module_selections_record_id_module_name_key":
		return "this module is already selected for this record"
	default:
		return "conflict: " + c.Detail
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// mapCommitError classifies an output.Client.CommitFiles error for
// sync_status purposes only -- it is never surfaced as an HTTP error
// response, per the two-phase "DB commits first, then git push" model: a
// commit failure (including output.ErrOutputModifiedExternally) leaves the
// already-saved record's sync_status=failed for retry, and the save HTTP
// response is still a success.
func mapCommitError(err error) string {
	if err == nil {
		return data.SyncStatusSynced
	}
	if errors.Is(err, output.ErrOutputModifiedExternally) {
		log.Printf("api: output repo modified externally, leaving sync_status=failed: %v", err)
	} else {
		log.Printf("api: commit to output repo failed, leaving sync_status=failed: %v", err)
	}
	return data.SyncStatusFailed
}
