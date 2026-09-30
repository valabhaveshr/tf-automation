package api

// logAudit is the write side of Phase 3/P3-C's audit trail: called by every
// handler that performs a state-changing action (record create/update/
// delete, catalog refresh, naming-template save, customer create) right
// after that action succeeds, per plan/ARCHITECTURE.md's Phase 3 mitigation
// "generate audit entries from the same code path that performs the
// action, not a separate best-effort logger" -- these calls sit directly in
// each handler, not behind generic middleware, so the actor/action/
// resource captured is always exactly what happened.

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"

	"tf-automation/backend/internal/auth"
	"tf-automation/backend/internal/data"
)

// strPtr returns a pointer to s, for the many optional *string fields
// (AuditLogEntry.ResourceID in particular) this package's handlers need to
// populate inline.
func strPtr(s string) *string { return &s }

// logAudit records one audit_log entry for a just-completed action. detail
// is marshaled to JSON if non-nil (pass a small struct/map -- e.g. the
// customer/project/env a record action touched); pass nil for actions with
// nothing further worth recording.
//
// Per standard practice for audit trails, a failure to insert the entry is
// logged as a warning and swallowed here -- it must never fail the HTTP
// response for the primary action that already succeeded. Audit logging is
// a record of what happened, not a gate on whether it's allowed to happen.
func (s *Server) logAudit(r *http.Request, action, resourceType string, resourceID *string, detail any) {
	claims, _ := auth.UserFromContext(r.Context())

	var actorUserID *uuid.UUID
	var actorUsername string
	if claims != nil {
		id := claims.UserID
		actorUserID = &id
		actorUsername = claims.Username
	}

	var detailJSON json.RawMessage
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			log.Printf("api: audit log: failed to marshal detail for action %q on %s: %v", action, resourceType, err)
		} else {
			detailJSON = b
		}
	}

	entry := &data.AuditLogEntry{
		ActorUserID:   actorUserID,
		ActorUsername: actorUsername,
		Action:        action,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		Detail:        detailJSON,
	}
	if err := s.Store.InsertAuditEntry(r.Context(), entry); err != nil {
		log.Printf("api: audit log: failed to record action %q on resource_type=%q: %v", action, resourceType, err)
	}
}
