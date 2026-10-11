package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"tf-automation/backend/internal/catalog"
	"tf-automation/backend/internal/data"
)

// handleCatalogList implements GET /api/catalog: list every module_schemas
// row currently flagged is_latest, mapped to the CatalogEntry shape.
//
// WS-A's Store does not expose a "list all latest module schemas" method
// (only per-module lookups: GetLatestModuleSchema, GetModuleSchemaByVersionTag)
// -- there is no such listing need anywhere else in WS-A/WS-D/WS-I. Rather
// than add a new method to store.go (reserved, per the task brief, for a
// transactional-scoping addition if needed), this uses Store.Pool()
// directly for a raw read query, exactly as store.go's own doc comment
// invites: "Pool exposes the underlying pool for callers that need to run
// raw queries ... this package doesn't otherwise provide."
func (s *Server) handleCatalogList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.Pool().Query(r.Context(),
		`SELECT module_name, version_tag, metadata FROM module_schemas WHERE is_latest = true ORDER BY module_name`)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()

	entries := make([]catalogEntry, 0)
	for rows.Next() {
		var moduleName, versionTag string
		var metadataRaw []byte
		if err := rows.Scan(&moduleName, &versionTag, &metadataRaw); err != nil {
			writeErr(w, err)
			return
		}
		var md catalog.ModuleMetadata
		if err := json.Unmarshal(metadataRaw, &md); err != nil {
			// A malformed cached metadata row shouldn't take down the whole
			// catalog listing; skip it (it would already have been flagged
			// at refresh time if RefreshModule's own json.Marshal step ever
			// failed, so this should not happen in practice).
			continue
		}
		entry := catalogEntry{
			ModuleName:       moduleName,
			DisplayName:      md.DisplayName,
			Category:         md.Category,
			Cloud:            md.Cloud,
			Icon:             md.Icon,
			Description:      md.Description,
			LatestVersionTag: versionTag,
		}
		if md.NamingResourceType != "" {
			nrt := md.NamingResourceType
			entry.NamingResourceType = &nrt
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, entries)
}

// handleCatalogSchema implements GET /api/catalog/{moduleName}/schema[?ref=],
// mounted as GET /catalog/* so moduleName may be a nested path.
func (s *Server) handleCatalogSchema(w http.ResponseWriter, r *http.Request) {
	moduleName, ok := moduleNameFromSchemaPath(chi.URLParam(r, "*"))
	if !ok {
		writeErr(w, data.ErrNotFound)
		return
	}
	ref := r.URL.Query().Get("ref")

	ms, err := s.resolveModuleSchema(r.Context(), moduleName, ref)
	if err != nil {
		writeErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, moduleSchemaResponse{
		ModuleName: ms.ModuleName,
		VersionTag: ms.VersionTag,
		Metadata:   json.RawMessage(ms.Metadata),
		Variables:  json.RawMessage(ms.Variables),
	})
}

// moduleNameFromSchemaPath extracts the module name from the wildcard part
// of GET /catalog/*, which must be "<moduleName>/schema". moduleName may
// itself contain slashes ("compute/instance/schema"), sent either as-is or
// URL-encoded as a single segment ("compute%2Finstance/schema"): chi routes
// on the raw path when it contains escapes, so the value is unescaped here.
func moduleNameFromSchemaPath(rest string) (string, bool) {
	if unescaped, err := url.PathUnescape(rest); err == nil {
		rest = unescaped
	}
	moduleName, ok := strings.CutSuffix(rest, "/schema")
	if !ok || moduleName == "" {
		return "", false
	}
	return moduleName, true
}

// resolveModuleSchema looks up the pinned version tag if ref is non-empty,
// else the latest cataloged schema. Returns data.ErrNotFound (mapped to 404
// by writeErr) if the module or version isn't catalog-eligible.
func (s *Server) resolveModuleSchema(ctx context.Context, moduleName, ref string) (*data.ModuleSchema, error) {
	if ref != "" {
		return s.Store.GetModuleSchemaByVersionTag(ctx, moduleName, ref)
	}
	return s.Store.GetLatestModuleSchema(ctx, moduleName)
}

// handleCatalogRefresh implements POST /api/catalog/refresh (admin-only,
// enforced by the RequireAuth+RequireAdmin middleware chained in
// server.go's Routes()).
//
// Per the task brief: "returns 202 immediately and runs it — synchronously
// is fine for Phase 1". A truly async job queue (dispatch RefreshAll to a
// background worker and let the caller poll/subscribe for completion) is a
// future improvement once Phase 1's needs outgrow a manual admin action
// completing within one HTTP request's timeout; RefreshAll's own cost is
// bounded by the modules repo's tag count, which is expected to stay small
// in Phase 1.
func (s *Server) handleCatalogRefresh(w http.ResponseWriter, r *http.Request) {
	issues, err := s.Catalog.RefreshAll(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	for _, issue := range issues {
		// Flagged-not-fatal per-module issues are logged, not surfaced as
		// an error response -- matching RefreshAll's own contract and the
		// "Catalog / schema parsing" edge-case table (one bad module never
		// fails the whole refresh).
		log.Printf("api: catalog refresh flagged module %q: %s", issue.ModuleName, issue.Reason)
	}
	s.logAudit(r, "catalog.refresh", "catalog", nil, nil)
	w.WriteHeader(http.StatusAccepted)
}

// handleWebhook implements POST /api/webhooks/github/modules: mounts
// catalog.WebhookHandler.HandleDelivery with no session auth middleware --
// it is HMAC-verified instead, per WS-D's design (catalog.VerifySignature
// inside HandleDelivery itself).
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := readAllLimited(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "could not read request body")
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	signature := r.Header.Get("X-Hub-Signature-256")

	if err := s.Webhook.HandleDelivery(r.Context(), eventType, deliveryID, body, signature); err != nil {
		// HandleDelivery returns a non-nil error only for conditions that
		// should be a non-2xx response (bad signature, malformed event
		// JSON) per its own doc comment. It has no exported sentinel for
		// "signature failed" specifically, so this distinguishes the one
		// documented failure mode (401, per contracts/openapi.yaml) from
		// anything else (400) by message content.
		if isSignatureError(err) {
			writeJSONError(w, http.StatusUnauthorized, "invalid HMAC signature")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func isSignatureError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "signature verification failed")
}
