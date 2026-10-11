package api

import (
	"net/http"
	"strconv"

	"tf-automation/backend/internal/data"
)

// handleListRecords implements GET /api/records[?customer=&project=&active=true],
// scoped to the caller's customer (platform admins: all customers, or
// ?customer_id=).
func (s *Server) handleListRecords(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTenant(r.Context(), r)
	if err != nil {
		writeErr(w, err)
		return
	}
	scope, err := t.recordScope(r)
	if err != nil {
		writeErr(w, err)
		return
	}

	q := r.URL.Query()

	filter := data.ListRecordsFilter{CustomerID: scope}
	if v := q.Get("customer"); v != "" {
		filter.CustomerName = &v
	}
	if v := q.Get("project"); v != "" {
		filter.ProjectName = &v
	}
	active := true // OpenAPI default
	if v := q.Get("active"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			writeErr(w, badRequest("invalid active query parameter"))
			return
		}
		active = parsed
	}
	filter.Active = &active

	records, err := s.Store.ListRecords(r.Context(), filter)
	if err != nil {
		writeErr(w, err)
		return
	}

	out := make([]recordSummary, 0, len(records))
	for _, rec := range records {
		out = append(out, recordSummary{
			ID:           rec.ID,
			CustomerID:   rec.CustomerID,
			CustomerName: rec.CustomerName,
			ProjectName:  rec.ProjectName,
			EnvName:      rec.EnvName,
			OutputPath:   rec.OutputPath,
			IsActive:     rec.IsActive,
			SyncStatus:   rec.SyncStatus,
			UpdatedAt:    rec.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetRecord implements GET /api/records/{id}.
func (s *Server) handleGetRecord(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeErr(w, badRequest("invalid record id"))
		return
	}
	t, err := s.loadTenant(r.Context(), r)
	if err != nil {
		writeErr(w, err)
		return
	}
	rec, err := s.loadAccessibleRecord(r.Context(), t, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	sels, err := s.Store.ListModuleSelectionsByRecord(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRecordResponse(rec, sels, nil))
}

// handleDeleteRecord implements DELETE /api/records/{id}: soft-delete only
// -- output repo files are never touched, per the locked decision.
func (s *Server) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeErr(w, badRequest("invalid record id"))
		return
	}
	t, err := s.loadTenant(r.Context(), r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.loadAccessibleRecord(r.Context(), t, id); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.SoftDeleteRecord(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	s.logAudit(r, "record.delete", "record", strPtr(id.String()), nil)
	w.WriteHeader(http.StatusNoContent)
}
