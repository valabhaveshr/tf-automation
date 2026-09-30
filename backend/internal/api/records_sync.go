package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"

	"tf-automation/backend/internal/catalog"
	"tf-automation/backend/internal/codegen"
	"tf-automation/backend/internal/data"
)

// commitAndUpdateStatus commits files to the output repo and persists the
// resulting sync_status/last_commit_sha via WS-A's narrow
// UpdateRecordSyncStatus (not the full optimistic-concurrency UpdateRecord
// path, since this isn't itself a user-initiated edit -- it runs after the
// DB transaction already committed). Per the locked two-phase "DB commits
// first, then git push" model (plan/ARCHITECTURE.md's transaction-flow
// sequence diagram), a commit failure here -- including
// output.ErrOutputModifiedExternally -- never fails the calling HTTP
// handler; it only leaves sync_status=failed for later retry via
// POST /records/{id}/retry-sync.
func (s *Server) commitAndUpdateStatus(ctx context.Context, recordID uuid.UUID, outputPath string, files map[string][]byte, message string) (lastCommitSHA *string, syncStatus string) {
	sha, err := s.Output.CommitFiles(ctx, s.OutputRepoOwner, s.OutputRepoName, outputPath, files, message)
	syncStatus = mapCommitError(err)
	if err == nil {
		lastCommitSHA = &sha
	}
	if _, updErr := s.Store.UpdateRecordSyncStatus(ctx, recordID, syncStatus, lastCommitSHA); updErr != nil {
		// The record was already saved successfully; failing to persist
		// the resulting sync_status is itself just logged -- the record
		// stays at whatever status the DB transaction left it at
		// (SyncStatusPending), which the stuck-pending reconciliation job
		// (a documented future piece, not built in this workstream) or a
		// manual retry-sync call can still resolve.
		log.Printf("api: failed to persist sync_status=%s for record %s: %v", syncStatus, recordID, updErr)
	}
	return lastCommitSHA, syncStatus
}

// handleSyncStatus implements GET /api/records/{id}/sync-status.
func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeErr(w, badRequest("invalid record id"))
		return
	}
	rec, err := s.Store.GetRecord(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, syncStatusResponse{SyncStatus: rec.SyncStatus, LastCommitSHA: rec.LastCommitSHA})
}

// handleRetrySync implements POST /api/records/{id}/retry-sync: re-attempts
// CommitFiles using the record's already-persisted module_selections,
// deliberately WITHOUT re-running naming allocation (the stored instance
// values already carry whatever name was allocated at the original save
// time -- re-allocating here would burn a second counter value for the
// same instance, which is exactly what plan/ARCHITECTURE.md's naming edge
// cases guard against).
func (s *Server) handleRetrySync(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeErr(w, badRequest("invalid record id"))
		return
	}

	ctx := r.Context()

	rec, err := s.Store.GetRecord(ctx, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if rec.SyncStatus != data.SyncStatusFailed {
		writeErr(w, conflictErr("record is not in a failed sync_status"))
		return
	}

	sels, err := s.Store.ListModuleSelectionsByRecord(ctx, id)
	if err != nil {
		writeErr(w, err)
		return
	}

	resolved := make([]*resolvedModule, 0, len(sels))
	for _, sel := range sels {
		ms, err := s.Store.GetModuleSchemaByVersionTag(ctx, sel.ModuleName, sel.PinnedVersion)
		if err != nil {
			writeErr(w, err)
			return
		}
		var meta catalog.ModuleMetadata
		_ = json.Unmarshal(ms.Metadata, &meta)
		var vars []codegen.VarSchema
		if err := json.Unmarshal(ms.Variables, &vars); err != nil {
			writeErr(w, fmt.Errorf("api: retry-sync: decoding cached variables for %q: %w", sel.ModuleName, err))
			return
		}
		var instances map[string]map[string]any
		if err := json.Unmarshal(sel.Instances, &instances); err != nil {
			writeErr(w, fmt.Errorf("api: retry-sync: decoding stored instances for %q: %w", sel.ModuleName, err))
			return
		}

		pinned := sel.PinnedVersion
		resolved = append(resolved, &resolvedModule{
			schema: ms,
			meta:   meta,
			vars:   vars,
			input: moduleInstanceInput{
				ModuleName:    sel.ModuleName,
				PinnedVersion: &pinned,
				Instances:     instances,
			},
		})
	}

	files, err := s.renderFiles(resolved)
	if err != nil {
		// Should not normally happen for previously-successfully-rendered,
		// already-persisted data; a failure here means the cached
		// schema/instances are now inconsistent. Not one of retry-sync's
		// documented response codes (202/404/409), so 500 with detail
		// logged for an operator to investigate.
		log.Printf("api: retry-sync record %s: re-render failed: %v", id, err)
		writeErr(w, fmt.Errorf("api: could not re-render record %s for retry: %w", id, err))
		return
	}

	if name, content, include := s.maybeRenderBackendFile(ctx, rec.OutputPath); include {
		files[name] = content
	}

	s.commitAndUpdateStatus(ctx, id, rec.OutputPath, files, fmt.Sprintf("tf-automation: retry-sync record %s", id))

	w.WriteHeader(http.StatusAccepted)
}
