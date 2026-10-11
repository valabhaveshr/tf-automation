package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tf-automation/backend/internal/data"
)

func parseUUIDParam(r *http.Request, key string) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, key))
}

// parseIfMatch parses the If-Match header's value as the RFC3339(Nano)
// timestamp string a client would have received back in a prior response's
// "updated_at" field (encoding/json's default time.Time marshaling), per
// the OpenAPI description: "The record's last-seen updated_at/version, for
// optimistic concurrency."
func parseIfMatch(raw string) (time.Time, error) {
	raw = strings.Trim(raw, `"`) // tolerate a quoted ETag-style value too
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func validateRecordInputShape(req recordInput) error {
	if strings.TrimSpace(req.CustomerName) == "" {
		return fmt.Errorf("customer_name is required")
	}
	if strings.TrimSpace(req.ProjectName) == "" {
		return fmt.Errorf("project_name is required")
	}
	if strings.TrimSpace(req.EnvName) == "" {
		return fmt.Errorf("env_name is required")
	}
	if len(req.ModuleSelections) == 0 {
		return fmt.Errorf("at least one module selection is required")
	}
	seen := make(map[string]bool, len(req.ModuleSelections))
	for _, sel := range req.ModuleSelections {
		if sel.ModuleName == "" {
			return fmt.Errorf("module_name is required for every module selection")
		}
		if seen[sel.ModuleName] {
			return fmt.Errorf("duplicate module selection %q", sel.ModuleName)
		}
		seen[sel.ModuleName] = true
		if len(sel.Instances) == 0 {
			return fmt.Errorf("module %q: at least one instance is required", sel.ModuleName)
		}
	}
	return nil
}

func commitMessage(action string, id uuid.UUID, req recordInput) string {
	return fmt.Sprintf("tf-automation: %s record %s (%s/%s/%s)", action, id, req.CustomerName, req.ProjectName, req.EnvName)
}

func toRecordResponse(rec *data.Record, sels []*data.ModuleSelection, warnings []string) recordResponse {
	out := recordResponse{
		recordSummary: recordSummary{
			ID:           rec.ID,
			CustomerID:   rec.CustomerID,
			CustomerName: rec.CustomerName,
			ProjectName:  rec.ProjectName,
			EnvName:      rec.EnvName,
			OutputPath:   rec.OutputPath,
			IsActive:     rec.IsActive,
			SyncStatus:   rec.SyncStatus,
			UpdatedAt:    rec.UpdatedAt,
		},
		LastCommitSHA: rec.LastCommitSHA,
		CreatedBy:     rec.CreatedBy,
		CreatedAt:     rec.CreatedAt,
		UpdatedBy:     rec.UpdatedBy,
		Warnings:      warnings,
	}
	for _, sel := range sels {
		var instances map[string]map[string]any
		_ = json.Unmarshal(sel.Instances, &instances)
		pv := sel.PinnedVersion
		out.ModuleSelections = append(out.ModuleSelections, moduleInstanceInput{
			ModuleName:    sel.ModuleName,
			PinnedVersion: &pv,
			Instances:     instances,
		})
	}
	return out
}
