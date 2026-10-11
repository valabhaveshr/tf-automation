package catalog

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// VerifySignature verifies a GitHub webhook payload against the
// X-Hub-Signature-256 header value, per GitHub's HMAC-SHA256 signing scheme
// (https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries).
// The comparison uses hmac.Equal for constant-time comparison, so it isn't
// vulnerable to a timing side-channel. Returns false (never panics/errors)
// for any malformed header -- missing "sha256=" prefix, non-hex digest, or
// wrong length -- since the caller only needs a plain yes/no.
func VerifySignature(payload []byte, signatureHeader, secret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return false
	}
	sigHex := strings.TrimPrefix(signatureHeader, prefix)
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}

// repositoryPayload is the subset of GitHub's webhook "repository" object
// this package needs.
type repositoryPayload struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// HandlePushEvent is the subset of GitHub's "push" webhook event payload
// this package needs.
// https://docs.github.com/en/webhooks/webhook-events-and-payloads#push
type HandlePushEvent struct {
	Ref        string             `json:"ref"` // fully-qualified, e.g. "refs/heads/main"
	Repository repositoryPayload  `json:"repository"`
}

// HandleCreateEvent is the subset of GitHub's "create" webhook event
// payload this package needs.
// https://docs.github.com/en/webhooks/webhook-events-and-payloads#create
type HandleCreateEvent struct {
	Ref        string            `json:"ref"`      // the tag or branch's short name (NOT ref-qualified, unlike push)
	RefType    string            `json:"ref_type"` // "tag" | "branch"
	Repository repositoryPayload `json:"repository"`
}

// SeenDeliveries dedups GitHub webhook deliveries by their
// X-GitHub-Delivery header, per plan/ARCHITECTURE.md's edge case
// "Duplicate/replayed webhook delivery -> Dedup by GitHub's delivery ID
// before reprocessing".
type SeenDeliveries interface {
	Seen(id string) bool
	Mark(id string)
}

// InMemorySeenDeliveries is a small sliding-window, in-memory
// SeenDeliveries. It retains delivery IDs for `window` duration -- enough
// to absorb GitHub's typical redelivery bursts (immediate automatic
// retries, occasional manual redelivery from the App's UI) without growing
// unboundedly over the process's lifetime. It is NOT durable across
// restarts or shared across multiple replicas; that would need a shared
// store (DB/Redis), which isn't built here since nothing in Phase 1 needs
// horizontal scaling of the webhook receiver -- noted rather than built
// speculatively.
type InMemorySeenDeliveries struct {
	mu     sync.Mutex
	window time.Duration
	seen   map[string]time.Time
}

// NewInMemorySeenDeliveries builds an InMemorySeenDeliveries retaining
// delivery IDs for the given window (e.g. 24*time.Hour).
func NewInMemorySeenDeliveries(window time.Duration) *InMemorySeenDeliveries {
	return &InMemorySeenDeliveries{
		window: window,
		seen:   make(map[string]time.Time),
	}
}

// Seen reports whether id has already been Mark-ed within the retention
// window. An empty id is always reported as not-seen (dedup is skipped
// rather than incorrectly collapsing every empty-header request together);
// real GitHub deliveries always carry a non-empty X-GitHub-Delivery header.
func (d *InMemorySeenDeliveries) Seen(id string) bool {
	if id == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.evictLocked()
	_, ok := d.seen[id]
	return ok
}

// Mark records id as seen, starting its retention window now.
func (d *InMemorySeenDeliveries) Mark(id string) {
	if id == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[id] = time.Now()
}

// evictLocked drops entries older than the window. Callers must hold d.mu.
func (d *InMemorySeenDeliveries) evictLocked() {
	cutoff := time.Now().Add(-d.window)
	for id, seenAt := range d.seen {
		if seenAt.Before(cutoff) {
			delete(d.seen, id)
		}
	}
}

// WebhookHandler wires the modules-repo GitHub webhook (push + create
// events) to Service.RefreshModule, per plan/ARCHITECTURE.md's Catalog
// refresh locked decision: "Primary: GitHub webhook on push to the modules
// repo and on tag creation (create events, ref_type=tag)."
type WebhookHandler struct {
	Service    *Service
	Secret     string
	TrackedRef string // e.g. "refs/heads/main" -- the push edge case's "configured tracked ref"
	Deliveries SeenDeliveries
	Logger     *log.Logger
}

// NewWebhookHandler builds a WebhookHandler. If deliveries is nil, delivery
// dedup is skipped entirely (every delivery is processed) -- callers wiring
// the real HTTP route (WS-G) should always pass an InMemorySeenDeliveries
// (or another SeenDeliveries) in production. If logger is nil, log.Default()
// is used.
func NewWebhookHandler(svc *Service, secret, trackedRef string, deliveries SeenDeliveries, logger *log.Logger) *WebhookHandler {
	if logger == nil {
		logger = log.Default()
	}
	return &WebhookHandler{
		Service:    svc,
		Secret:     secret,
		TrackedRef: trackedRef,
		Deliveries: deliveries,
		Logger:     logger,
	}
}

// HandleDelivery processes one webhook delivery end-to-end: verifies the
// HMAC signature, dedups by delivery ID, and dispatches to the push/create
// handling based on eventType (the X-GitHub-Event header value).
//
// It returns a non-nil error only for conditions the HTTP layer (WS-G)
// should turn into a non-2xx response -- a bad/missing signature, or
// malformed event JSON. A successfully-processed-but-ignored event (wrong
// branch, a non-tag create, an unrecognized event type, a duplicate
// delivery) returns nil: per GitHub's webhook contract, the receiver should
// still ack with 2xx so GitHub doesn't keep retrying a delivery it
// correctly decided not to act on.
func (h *WebhookHandler) HandleDelivery(ctx context.Context, eventType, deliveryID string, payload []byte, signatureHeader string) error {
	if !VerifySignature(payload, signatureHeader, h.Secret) {
		return fmt.Errorf("catalog: webhook: signature verification failed")
	}

	if h.Deliveries != nil {
		if h.Deliveries.Seen(deliveryID) {
			h.Logger.Printf("catalog: webhook: ignoring duplicate delivery %s", deliveryID)
			return nil
		}
		h.Deliveries.Mark(deliveryID)
	}

	switch eventType {
	case "push":
		return h.handlePush(ctx, payload)
	case "create":
		return h.handleCreate(ctx, payload)
	default:
		// Any other event type the route might receive (e.g. "ping" sent
		// once when the webhook is first configured) is simply acknowledged
		// and ignored -- not catalog-relevant.
		return nil
	}
}

// handlePush reacts only to a push on the configured tracked branch, per
// the edge case "Webhook fires for a push to a non-tracked branch (e.g. a
// PR branch) -> Receiver only reacts to pushes on the configured tracked
// ref". Even for a tracked-branch push, this is a deliberate no-op: catalog
// entries are entirely tag-driven (the "Module versioning" locked
// decision), so a push to the tracked branch by itself never changes what's
// cataloged -- only a NEW <module-name>/vX.Y.Z TAG does, which arrives as a
// separate "create" event handled by handleCreate. A push with no
// accompanying tag simply means the catalog doesn't change yet; the module
// author still needs to cut a release tag before the change becomes
// catalog-visible. This is still logged for observability (so a
// tracked-branch push can be correlated with the tag-create event expected
// to follow it), without doing any refresh work -- building more here would
// be speculative since nothing in Phase 1 depends on push-triggered catalog
// refresh.
func (h *WebhookHandler) handlePush(ctx context.Context, payload []byte) error {
	var p HandlePushEvent
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("catalog: webhook: decoding push payload: %w", err)
	}

	if p.Ref != h.TrackedRef {
		h.Logger.Printf("catalog: webhook: ignoring push to non-tracked ref %q (tracked: %q)", p.Ref, h.TrackedRef)
		return nil
	}

	h.Logger.Printf("catalog: webhook: push to tracked ref %q received (no-op; catalog is tag-driven, see handleCreate)", p.Ref)
	return nil
}

// handleCreate reacts to a tag-create event whose tag matches the
// "<module-name>/vX.Y.Z" convention by refreshing exactly that one module --
// the "cheap, targeted" refresh the task calls for, as opposed to a full
// RefreshAll. A branch-create event, or a tag that doesn't match the
// convention, is simply ignored (not catalog-relevant).
func (h *WebhookHandler) handleCreate(ctx context.Context, payload []byte) error {
	var p HandleCreateEvent
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("catalog: webhook: decoding create payload: %w", err)
	}

	if p.RefType != "tag" {
		return nil
	}

	moduleName, _, ok := moduleNameFromTag(p.Ref, h.Service.ModulesPath)
	if !ok {
		h.Logger.Printf("catalog: webhook: ignoring non-module tag %q", p.Ref)
		return nil
	}

	if err := h.Service.RefreshModule(ctx, moduleName); err != nil {
		var issue *ModuleIssue
		if asModuleIssue(err, &issue) {
			// Flagged, not fatal: per the edge-case table, this module is
			// excluded from the catalog until fixed, but the webhook
			// delivery itself was handled correctly -- still ack 2xx.
			h.Logger.Printf("catalog: webhook: refresh of module %q flagged: %s", issue.ModuleName, issue.Reason)
			return nil
		}
		return fmt.Errorf("catalog: webhook: refreshing module %q: %w", moduleName, err)
	}

	h.Logger.Printf("catalog: webhook: refreshed module %q from tag %q", moduleName, p.Ref)
	return nil
}
