package catalog

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
	"testing"
	"time"
)

func sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	payload := []byte(`{"ref":"refs/heads/main"}`)
	secret := "s3cr3t"

	valid := sign(payload, secret)
	if !VerifySignature(payload, valid, secret) {
		t.Errorf("expected a valid signature to verify")
	}

	if VerifySignature(payload, valid, "wrong-secret") {
		t.Errorf("expected verification to fail against the wrong secret")
	}

	tampered := []byte(`{"ref":"refs/heads/evil"}`)
	if VerifySignature(tampered, valid, secret) {
		t.Errorf("expected verification to fail for a tampered payload")
	}

	if VerifySignature(payload, "not-even-hex-prefixed", secret) {
		t.Errorf("expected verification to fail for a malformed header")
	}

	if VerifySignature(payload, "sha256=zzzz", secret) {
		t.Errorf("expected verification to fail for a non-hex digest")
	}

	if VerifySignature(payload, "", secret) {
		t.Errorf("expected verification to fail for an empty header")
	}
}

// newTestWebhookHandler builds a WebhookHandler wired to an in-memory
// Service (fake source + fake store) with logging captured to buf so tests
// can assert on log content where behavior is otherwise a deliberate no-op.
func newTestWebhookHandler(t *testing.T, src *fakeSource, store *fakeSchemaStore, trackedRef string) (*WebhookHandler, *bytes.Buffer) {
	t.Helper()
	svc := &Service{Source: src, Store: store, ModulesPath: "modules"}
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	h := NewWebhookHandler(svc, "s3cr3t", trackedRef, NewInMemorySeenDeliveries(time.Hour), logger)
	return h, &buf
}

func TestWebhookHandler_CreateEvent_RefreshesTargetedModule(t *testing.T) {
	src := newFakeSource()
	src.addTag("module-a/v1.0.0")
	src.addFile("module-a/v1.0.0", "modules/module-a/variables.tf", validVarsTF)
	src.addFile("module-a/v1.0.0", "modules/module-a/metadata.yaml", validMetadataYAML("module-a"))
	store := newFakeSchemaStore()

	h, _ := newTestWebhookHandler(t, src, store, "refs/heads/main")

	payload := []byte(`{"ref":"module-a/v1.0.0","ref_type":"tag","repository":{"name":"modules","full_name":"org/modules"}}`)
	sig := sign(payload, h.Secret)

	if err := h.HandleDelivery(context.Background(), "create", "delivery-1", payload, sig); err != nil {
		t.Fatalf("HandleDelivery returned unexpected error: %v", err)
	}
	if store.latest("module-a") == nil {
		t.Fatalf("expected module-a to have been refreshed by the create event")
	}
}

func TestWebhookHandler_CreateEvent_BranchRefTypeIgnored(t *testing.T) {
	src := newFakeSource()
	store := newFakeSchemaStore()
	h, _ := newTestWebhookHandler(t, src, store, "refs/heads/main")

	payload := []byte(`{"ref":"some-branch","ref_type":"branch","repository":{"name":"modules"}}`)
	sig := sign(payload, h.Secret)

	if err := h.HandleDelivery(context.Background(), "create", "delivery-1", payload, sig); err != nil {
		t.Fatalf("HandleDelivery returned unexpected error: %v", err)
	}
	if store.count() != 0 {
		t.Fatalf("expected no catalog activity for a branch-create event, got %d rows", store.count())
	}
}

func TestWebhookHandler_PushEvent_NonTrackedBranchIsNoOp(t *testing.T) {
	src := newFakeSource()
	store := newFakeSchemaStore()
	h, buf := newTestWebhookHandler(t, src, store, "refs/heads/main")

	payload := []byte(`{"ref":"refs/heads/feature-branch","repository":{"name":"modules"}}`)
	sig := sign(payload, h.Secret)

	if err := h.HandleDelivery(context.Background(), "push", "delivery-1", payload, sig); err != nil {
		t.Fatalf("HandleDelivery returned unexpected error: %v", err)
	}
	if store.count() != 0 {
		t.Fatalf("expected no catalog activity for a push to a non-tracked branch, got %d rows", store.count())
	}
	if !strings.Contains(buf.String(), "ignoring push to non-tracked ref") {
		t.Errorf("expected a log message noting the non-tracked ref was ignored, got: %s", buf.String())
	}
}

func TestWebhookHandler_PushEvent_TrackedBranchIsLoggedNoOp(t *testing.T) {
	src := newFakeSource()
	store := newFakeSchemaStore()
	h, buf := newTestWebhookHandler(t, src, store, "refs/heads/main")

	payload := []byte(`{"ref":"refs/heads/main","repository":{"name":"modules"}}`)
	sig := sign(payload, h.Secret)

	if err := h.HandleDelivery(context.Background(), "push", "delivery-1", payload, sig); err != nil {
		t.Fatalf("HandleDelivery returned unexpected error: %v", err)
	}
	// Per plan/ARCHITECTURE.md: catalog entries are tag-driven, so even a
	// tracked-branch push must not itself trigger any refresh.
	if store.count() != 0 {
		t.Fatalf("expected no catalog activity for a tracked-branch push (tag-driven catalog), got %d rows", store.count())
	}
	if !strings.Contains(buf.String(), "no-op") {
		t.Errorf("expected a log message noting the tracked-branch push is a no-op, got: %s", buf.String())
	}
}

func TestWebhookHandler_BadSignatureRejected(t *testing.T) {
	src := newFakeSource()
	store := newFakeSchemaStore()
	h, _ := newTestWebhookHandler(t, src, store, "refs/heads/main")

	payload := []byte(`{"ref":"refs/heads/main","repository":{"name":"modules"}}`)

	err := h.HandleDelivery(context.Background(), "push", "delivery-1", payload, "sha256=deadbeef")
	if err == nil {
		t.Fatalf("expected an error for a bad signature")
	}
}

func TestWebhookHandler_DuplicateDeliveryIsSkipped(t *testing.T) {
	src := newFakeSource()
	src.addTag("module-a/v1.0.0")
	src.addFile("module-a/v1.0.0", "modules/module-a/variables.tf", validVarsTF)
	src.addFile("module-a/v1.0.0", "modules/module-a/metadata.yaml", validMetadataYAML("module-a"))

	store := newFakeSchemaStore()
	h, buf := newTestWebhookHandler(t, src, store, "refs/heads/main")

	payload := []byte(`{"ref":"module-a/v1.0.0","ref_type":"tag","repository":{"name":"modules"}}`)
	sig := sign(payload, h.Secret)

	if err := h.HandleDelivery(context.Background(), "create", "dup-delivery", payload, sig); err != nil {
		t.Fatalf("first delivery: unexpected error: %v", err)
	}
	if store.count() != 1 {
		t.Fatalf("expected 1 cataloged row after first delivery, got %d", store.count())
	}

	// Replay the exact same delivery ID: must be skipped, not reprocessed.
	// Mutate the underlying store row count check by re-checking count stays 1.
	if err := h.HandleDelivery(context.Background(), "create", "dup-delivery", payload, sig); err != nil {
		t.Fatalf("duplicate delivery: unexpected error: %v", err)
	}
	if store.count() != 1 {
		t.Fatalf("expected duplicate delivery to be skipped, still expected 1 row, got %d", store.count())
	}
	if !strings.Contains(buf.String(), "ignoring duplicate delivery") {
		t.Errorf("expected a log message noting the duplicate delivery was ignored, got: %s", buf.String())
	}
}

func TestInMemorySeenDeliveries(t *testing.T) {
	d := NewInMemorySeenDeliveries(time.Hour)

	if d.Seen("a") {
		t.Errorf("expected 'a' to be unseen initially")
	}
	d.Mark("a")
	if !d.Seen("a") {
		t.Errorf("expected 'a' to be seen after Mark")
	}
	if d.Seen("b") {
		t.Errorf("expected 'b' to remain unseen")
	}
	// Empty delivery ID is never considered "seen" -- dedup intentionally
	// skipped rather than collapsing all empty-header requests together.
	if d.Seen("") {
		t.Errorf("expected empty id to never be reported as seen")
	}
}

func TestWebhookHandler_CreateEvent_TagWithModulesDirPrefix(t *testing.T) {
	src := newFakeSource()
	src.addTag("modules/compute/instance/v1.0.0")
	src.addFile("modules/compute/instance/v1.0.0", "modules/compute/instance/variables.tf", validVarsTF)
	src.addFile("modules/compute/instance/v1.0.0", "modules/compute/instance/metadata.yaml", validMetadataYAML("instance"))
	store := newFakeSchemaStore()

	h, _ := newTestWebhookHandler(t, src, store, "refs/heads/main")

	payload := []byte(`{"ref":"modules/compute/instance/v1.0.0","ref_type":"tag","repository":{"name":"modules","full_name":"org/modules"}}`)
	sig := sign(payload, h.Secret)

	if err := h.HandleDelivery(context.Background(), "create", "delivery-1", payload, sig); err != nil {
		t.Fatalf("HandleDelivery returned unexpected error: %v", err)
	}
	if store.latest("compute/instance") == nil {
		t.Fatalf("expected compute/instance to have been refreshed by the create event")
	}
}
