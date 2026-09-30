package data

// See roles_test.go's package doc comment for why these tests require a
// real Postgres instance (TEST_DATABASE_URL) and skip themselves otherwise.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestAuditLog_InsertAndList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "audit-test")

	actorID := u.ID
	e1 := &AuditLogEntry{
		ActorUserID:   &actorID,
		ActorUsername: u.Username,
		Action:        "record.create",
		ResourceType:  "record",
		ResourceID:    strPtr("rec-1"),
		Detail:        json.RawMessage(`{"foo":"bar"}`),
	}
	if err := s.InsertAuditEntry(ctx, e1); err != nil {
		t.Fatalf("InsertAuditEntry(e1): %v", err)
	}
	if e1.ID == uuid.Nil {
		t.Fatal("expected InsertAuditEntry to populate ID")
	}
	if e1.CreatedAt.IsZero() {
		t.Fatal("expected InsertAuditEntry to populate CreatedAt")
	}

	// System-initiated action: no actor_user_id, no resource_id, no detail.
	e2 := &AuditLogEntry{
		ActorUsername: u.Username,
		Action:        "catalog.refresh",
		ResourceType:  "catalog",
	}
	if err := s.InsertAuditEntry(ctx, e2); err != nil {
		t.Fatalf("InsertAuditEntry(e2): %v", err)
	}
	if e2.ActorUserID != nil {
		t.Fatal("expected e2.ActorUserID to remain nil")
	}

	// Filter by resource_type.
	got, err := s.ListAuditLog(ctx, AuditLogFilter{ResourceType: "record"})
	if err != nil {
		t.Fatalf("ListAuditLog(resource_type=record): %v", err)
	}
	if !containsAuditID(got, e1.ID) {
		t.Fatalf("expected e1 in resource_type=record results (%d entries)", len(got))
	}
	if containsAuditID(got, e2.ID) {
		t.Fatal("did not expect e2 (resource_type=catalog) in resource_type=record results")
	}

	// Filter by resource_type + resource_id together.
	got, err = s.ListAuditLog(ctx, AuditLogFilter{ResourceType: "record", ResourceID: "rec-1"})
	if err != nil {
		t.Fatalf("ListAuditLog(resource_type=record, resource_id=rec-1): %v", err)
	}
	if !containsAuditID(got, e1.ID) {
		t.Fatal("expected e1 in resource_type=record,resource_id=rec-1 results")
	}

	// Filter by actor.
	got, err = s.ListAuditLog(ctx, AuditLogFilter{ActorUsername: u.Username})
	if err != nil {
		t.Fatalf("ListAuditLog(actor): %v", err)
	}
	if !containsAuditID(got, e1.ID) || !containsAuditID(got, e2.ID) {
		t.Fatalf("expected both e1 and e2 for actor=%s (%d entries)", u.Username, len(got))
	}

	// Cursor: Before = e1.CreatedAt excludes e1 (strict <).
	before := e1.CreatedAt
	got, err = s.ListAuditLog(ctx, AuditLogFilter{ActorUsername: u.Username, Before: &before})
	if err != nil {
		t.Fatalf("ListAuditLog(before=e1.CreatedAt): %v", err)
	}
	if containsAuditID(got, e1.ID) {
		t.Fatal("expected e1 excluded when Before = e1.CreatedAt (strict <)")
	}

	// Limit is honored.
	got, err = s.ListAuditLog(ctx, AuditLogFilter{ActorUsername: u.Username, Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditLog(limit=1): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected Limit=1 to return exactly 1 entry, got %d", len(got))
	}

	// Limit is clamped to maxAuditLogLimit, not passed through raw.
	got, err = s.ListAuditLog(ctx, AuditLogFilter{ActorUsername: u.Username, Limit: 10000})
	if err != nil {
		t.Fatalf("ListAuditLog(limit=10000): %v", err)
	}
	if len(got) > maxAuditLogLimit {
		t.Fatalf("expected at most %d entries, got %d", maxAuditLogLimit, len(got))
	}
}

func strPtr(s string) *string { return &s }

func containsAuditID(entries []*AuditLogEntry, id uuid.UUID) bool {
	for _, e := range entries {
		if e.ID == id {
			return true
		}
	}
	return false
}
