package data

import (
	"context"
	"fmt"
	"time"
)

const auditLogColumns = `id, actor_user_id, actor_username, action, resource_type, resource_id, detail, created_at`

func scanAuditLogEntry(row rowScanner, e *AuditLogEntry) error {
	return row.Scan(&e.ID, &e.ActorUserID, &e.ActorUsername, &e.Action, &e.ResourceType,
		&e.ResourceID, &e.Detail, &e.CreatedAt)
}

// InsertAuditEntry records one audit_log row. Callers set ActorUserID (nil
// for system-initiated actions), ActorUsername, Action, ResourceType, and
// optionally ResourceID/Detail; ID and CreatedAt are DB-assigned and
// populated on e after a successful insert.
func (s *Store) InsertAuditEntry(ctx context.Context, e *AuditLogEntry) error {
	q := `INSERT INTO audit_log (actor_user_id, actor_username, action, resource_type, resource_id, detail)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + auditLogColumns
	row := s.pool.QueryRow(ctx, q, e.ActorUserID, e.ActorUsername, e.Action, e.ResourceType, e.ResourceID, e.Detail)
	if err := scanAuditLogEntry(row, e); err != nil {
		return fmt.Errorf("data: insert audit entry: %w", err)
	}
	return nil
}

// Default/max page size for ListAuditLog, matching GET /admin/audit-log's
// `limit` query param contract (contracts/openapi.yaml): default 50, max
// 200.
const (
	defaultAuditLogLimit = 50
	maxAuditLogLimit     = 200
)

// AuditLogFilter narrows a ListAuditLog query to match GET /admin/audit-log's
// query params (contracts/openapi.yaml): resource_type, resource_id, actor,
// limit, before. Zero-value string fields and a nil Before are not filtered
// on; Limit <= 0 falls back to defaultAuditLogLimit and is capped at
// maxAuditLogLimit.
type AuditLogFilter struct {
	ResourceType  string
	ResourceID    string
	ActorUsername string
	Before        *time.Time // cursor: only entries created strictly before this timestamp
	Limit         int
}

// ListAuditLog returns audit_log entries matching filter, newest first.
func (s *Store) ListAuditLog(ctx context.Context, filter AuditLogFilter) ([]*AuditLogEntry, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultAuditLogLimit
	}
	if limit > maxAuditLogLimit {
		limit = maxAuditLogLimit
	}

	q := `SELECT ` + auditLogColumns + ` FROM audit_log WHERE TRUE`
	var args []any

	if filter.ResourceType != "" {
		args = append(args, filter.ResourceType)
		q += fmt.Sprintf(" AND resource_type = $%d", len(args))
	}
	if filter.ResourceID != "" {
		args = append(args, filter.ResourceID)
		q += fmt.Sprintf(" AND resource_id = $%d", len(args))
	}
	if filter.ActorUsername != "" {
		args = append(args, filter.ActorUsername)
		q += fmt.Sprintf(" AND actor_username = $%d", len(args))
	}
	if filter.Before != nil {
		args = append(args, *filter.Before)
		q += fmt.Sprintf(" AND created_at < $%d", len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("data: list audit log: %w", err)
	}
	defer rows.Close()

	var out []*AuditLogEntry
	for rows.Next() {
		e := &AuditLogEntry{}
		if err := scanAuditLogEntry(rows, e); err != nil {
			return nil, fmt.Errorf("data: list audit log: scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: list audit log: %w", err)
	}
	return out, nil
}
