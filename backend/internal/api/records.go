package api

import (
	"log"
	"net/http"

	"github.com/google/uuid"

	"tf-automation/backend/internal/data"
)

// ---------------------------------------------------------------------
// Preview: POST /api/records/preview and PUT /api/records/{id}/preview.
//
// Follows plan/ARCHITECTURE.md's transaction-flow sequence diagram's
// preview leg exactly: resolve schemas -> Suggest (never Allocate) a
// tentative name per naming-enabled instance -> merge into empty target
// fields -> codegen.RenderRootFiles -> (edit-preview only) diff against
// what's currently committed. Nothing is persisted or committed to git.
// ---------------------------------------------------------------------

// handlePreviewCreate implements POST /api/records/preview.
func (s *Server) handlePreviewCreate(w http.ResponseWriter, r *http.Request) {
	s.handlePreview(w, r, nil)
}

// handlePreviewEdit implements PUT /api/records/{id}/preview.
func (s *Server) handlePreviewEdit(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeErr(w, badRequest("invalid record id"))
		return
	}
	s.handlePreview(w, r, &id)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request, recordID *uuid.UUID) {
	var req recordInput
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	if err := validateRecordInputShape(req); err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}

	ctx := r.Context()

	resolved, err := s.resolveModules(ctx, req.ModuleSelections)
	if err != nil {
		writeErr(w, err)
		return
	}

	cust, err := s.resolveCustomer(ctx, req.CustomerName)
	if err != nil {
		writeErr(w, err)
		return
	}

	suggestedNames := make(map[string]string)
	var warnings []string
	for _, rm := range resolved {
		sn, wn, err := s.applyNaming(ctx, namingModeSuggest, req.EnvName, req.ProjectName, cust, &rm.input, rm.meta)
		if err != nil {
			writeErr(w, err)
			return
		}
		for k, v := range sn {
			suggestedNames[k] = v
		}
		warnings = append(warnings, wn...)
	}

	files, err := s.renderFiles(resolved)
	if err != nil {
		writeErr(w, err)
		return
	}

	outputPath := buildOutputPath(req.CustomerName, req.ProjectName, req.EnvName)

	// Mirror the commit-time backend.tf decision so the preview accurately
	// shows whether backend.tf will be (re)generated on this save --
	// "preview and save share one code path, guaranteeing byte-parity"
	// (locked decision).
	if name, content, include := s.maybeRenderBackendFile(ctx, outputPath); include {
		files[name] = content
	}

	result := previewResult{
		Files:          filesToStrings(files),
		SuggestedNames: suggestedNames,
		Warnings:       warnings,
	}

	if recordID != nil {
		diff, err := s.buildDiff(ctx, outputPath, files)
		if err != nil {
			// A diff failure (e.g. a transient GitHub error) shouldn't
			// fail the whole preview -- the generated files themselves are
			// still valid and useful on their own.
			log.Printf("api: preview diff failed for record %s: %v", *recordID, err)
		} else {
			result.Diff = &diff
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------
// Create: POST /api/records.
// ---------------------------------------------------------------------

func (s *Server) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	var req recordInput
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	if err := validateRecordInputShape(req); err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}

	ctx := r.Context()

	resolved, err := s.resolveModules(ctx, req.ModuleSelections)
	if err != nil {
		writeErr(w, err)
		return
	}

	cust, err := s.resolveCustomer(ctx, req.CustomerName)
	if err != nil {
		writeErr(w, err)
		return
	}

	// Allocate (burns counters) for every naming-enabled module BEFORE the
	// DB transaction. See records_tx.go's doc comment for why this can't
	// be nested into that transaction without modifying WS-A/WS-I, and the
	// task report for the accepted non-atomic gap this implies: if the
	// insert below fails after a counter was allocated here, that counter
	// value is burned and not reclaimed.
	var warnings []string
	for _, rm := range resolved {
		_, wn, err := s.applyNaming(ctx, namingModeAllocate, req.EnvName, req.ProjectName, cust, &rm.input, rm.meta)
		if err != nil {
			writeErr(w, err)
			return
		}
		warnings = append(warnings, wn...)
	}

	files, err := s.renderFiles(resolved)
	if err != nil {
		log.Printf("api: create record: codegen failed after naming allocation for customer=%q project=%q env=%q -- any counters allocated above are NOT rolled back: %v",
			req.CustomerName, req.ProjectName, req.EnvName, err)
		writeErr(w, err)
		return
	}

	outputPath := buildOutputPath(req.CustomerName, req.ProjectName, req.EnvName)

	var customerID *uuid.UUID
	if cust.found {
		id := cust.id
		customerID = &id
	}

	rec := &data.Record{
		CustomerName: req.CustomerName,
		CustomerID:   customerID,
		ProjectName:  req.ProjectName,
		EnvName:      req.EnvName,
		OutputPath:   outputPath,
		CreatedBy:    currentUsername(r),
		UpdatedBy:    currentUsername(r),
		SyncStatus:   data.SyncStatusPending,
	}

	savedRecord, savedSelections, err := s.createRecordTx(ctx, rec, resolved)
	if err != nil {
		log.Printf("api: create record: DB transaction failed after naming allocation for customer=%q project=%q env=%q -- any counters allocated above are NOT rolled back: %v",
			req.CustomerName, req.ProjectName, req.EnvName, err)
		writeErr(w, err)
		return
	}

	// Only include backend.tf in this first commit if it doesn't already
	// exist at this path (it structurally can't for a genuinely brand-new
	// record, but the check is shared with update/retry-sync so it's
	// exercised identically everywhere).
	if name, content, include := s.maybeRenderBackendFile(ctx, outputPath); include {
		files[name] = content
	}

	sha, syncStatus := s.commitAndUpdateStatus(ctx, savedRecord.ID, outputPath, files, commitMessage("create", savedRecord.ID, req))
	savedRecord.SyncStatus = syncStatus
	savedRecord.LastCommitSHA = sha

	s.logAudit(r, "record.create", "record", strPtr(savedRecord.ID.String()), map[string]string{
		"customer_name": req.CustomerName,
		"project_name":  req.ProjectName,
		"env_name":      req.EnvName,
	})

	writeJSON(w, http.StatusCreated, toRecordResponse(savedRecord, savedSelections, warnings))
}

// ---------------------------------------------------------------------
// Update: PUT /api/records/{id}.
// ---------------------------------------------------------------------

func (s *Server) handleUpdateRecord(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeErr(w, badRequest("invalid record id"))
		return
	}

	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		writeErr(w, badRequest("If-Match header is required"))
		return
	}
	expectedUpdatedAt, err := parseIfMatch(ifMatch)
	if err != nil {
		writeErr(w, badRequest("invalid If-Match header: expected the record's last-seen updated_at timestamp"))
		return
	}

	var req recordInput
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	if err := validateRecordInputShape(req); err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}

	ctx := r.Context()

	existing, err := s.Store.GetRecord(ctx, id)
	if err != nil {
		writeErr(w, err)
		return
	}

	resolved, err := s.resolveModules(ctx, req.ModuleSelections)
	if err != nil {
		writeErr(w, err)
		return
	}

	cust, err := s.resolveCustomer(ctx, req.CustomerName)
	if err != nil {
		writeErr(w, err)
		return
	}

	// Same non-atomic-with-DB-insert caveat as create; see that handler's
	// comment and records_tx.go's doc comment.
	var warnings []string
	for _, rm := range resolved {
		_, wn, err := s.applyNaming(ctx, namingModeAllocate, req.EnvName, req.ProjectName, cust, &rm.input, rm.meta)
		if err != nil {
			writeErr(w, err)
			return
		}
		warnings = append(warnings, wn...)
	}

	files, err := s.renderFiles(resolved)
	if err != nil {
		log.Printf("api: update record %s: codegen failed after naming allocation -- any counters allocated above are NOT rolled back: %v", id, err)
		writeErr(w, err)
		return
	}

	outputPath := buildOutputPath(req.CustomerName, req.ProjectName, req.EnvName)

	var customerID *uuid.UUID
	if cust.found {
		cid := cust.id
		customerID = &cid
	}

	updated := &data.Record{
		ID:            existing.ID,
		CustomerName:  req.CustomerName,
		CustomerID:    customerID,
		ProjectName:   req.ProjectName,
		EnvName:       req.EnvName,
		OutputPath:    outputPath,
		UpdatedBy:     currentUsername(r),
		LastCommitSHA: existing.LastCommitSHA,
		SyncStatus:    existing.SyncStatus,
	}

	savedRecord, savedSelections, err := s.updateRecordTx(ctx, updated, expectedUpdatedAt, resolved)
	if err != nil {
		log.Printf("api: update record %s: DB transaction failed after naming allocation -- any counters allocated above are NOT rolled back: %v", id, err)
		writeErr(w, err)
		return
	}

	// Never regenerate/overwrite an existing backend.tf on routine edits;
	// maybeRenderBackendFile enforces that by checking existence first.
	if name, content, include := s.maybeRenderBackendFile(ctx, outputPath); include {
		files[name] = content
	}

	sha, syncStatus := s.commitAndUpdateStatus(ctx, savedRecord.ID, outputPath, files, commitMessage("update", savedRecord.ID, req))
	savedRecord.SyncStatus = syncStatus
	savedRecord.LastCommitSHA = sha

	s.logAudit(r, "record.update", "record", strPtr(savedRecord.ID.String()), map[string]string{
		"customer_name": req.CustomerName,
		"project_name":  req.ProjectName,
		"env_name":      req.EnvName,
	})

	writeJSON(w, http.StatusOK, toRecordResponse(savedRecord, savedSelections, warnings))
}
