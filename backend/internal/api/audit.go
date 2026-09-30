package api

// handleAuditLog implements GET /api/admin/audit-log (contracts/openapi.yaml,
// added Phase 3/P3-C), the read side of the audit trail. Write side (the
// InsertAuditEntry calls after each state-changing action) lives alongside
// each of those actions' handlers -- see audit_helpers.go's logAudit.

import (
	"net/http"
	"strconv"
	"time"

	"tf-automation/backend/internal/data"
)

// handleAuditLog is admin-only, enforced by the RequireAuth+RequireAdmin
// middleware chained onto /admin/* in server.go's Routes() -- same
// enforcement point as the other /admin/* endpoints.
func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := data.AuditLogFilter{
		ResourceType:  q.Get("resource_type"),
		ResourceID:    q.Get("resource_id"),
		ActorUsername: q.Get("actor"),
	}

	if v := q.Get("limit"); v != "" {
		limit, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, badRequest("invalid limit query parameter"))
			return
		}
		filter.Limit = limit
	}

	if v := q.Get("before"); v != "" {
		before, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, badRequest("invalid before query parameter: expected an RFC3339 date-time"))
			return
		}
		filter.Before = &before
	}

	entries, err := s.Store.ListAuditLog(r.Context(), filter)
	if err != nil {
		writeErr(w, err)
		return
	}

	// data.AuditLogEntry's JSON tags already mirror
	// #/components/schemas/AuditLogEntry exactly (id, actor_user_id,
	// actor_username, action, resource_type, resource_id, detail,
	// created_at) -- no separate wire DTO needed here, unlike most of this
	// package's other response types.
	out := make([]*data.AuditLogEntry, 0, len(entries))
	out = append(out, entries...)
	writeJSON(w, http.StatusOK, out)
}
