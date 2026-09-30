package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const recordColumns = `id, customer_name, customer_id, project_name, env_name, output_path,
	is_active, created_by, created_at, updated_by, updated_at, last_commit_sha, sync_status`

func scanRecord(row rowScanner, r *Record) error {
	return row.Scan(&r.ID, &r.CustomerName, &r.CustomerID, &r.ProjectName, &r.EnvName, &r.OutputPath,
		&r.IsActive, &r.CreatedBy, &r.CreatedAt, &r.UpdatedBy, &r.UpdatedAt, &r.LastCommitSHA, &r.SyncStatus)
}

// CreateRecord inserts a new record. Callers should set CustomerName,
// CustomerID (once the customer registry is resolved), ProjectName,
// EnvName, OutputPath, CreatedBy and UpdatedBy (equal on create) before
// calling; SyncStatus defaults to "pending" when left empty.
//
// A violation of the (customer_name, project_name, env_name) UNIQUE
// constraint is returned as a *ConflictError (unwraps to ErrConflict) so
// the API layer can surface "this env already exists" rather than a raw
// 500, per plan/ARCHITECTURE.md's storage/consistency edge cases.
func (s *Store) CreateRecord(ctx context.Context, r *Record) (*Record, error) {
	syncStatus := r.SyncStatus
	if syncStatus == "" {
		syncStatus = SyncStatusPending
	}
	q := `INSERT INTO records (customer_name, customer_id, project_name, env_name, output_path,
			created_by, updated_by, sync_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + recordColumns

	row := s.pool.QueryRow(ctx, q,
		r.CustomerName, r.CustomerID, r.ProjectName, r.EnvName, r.OutputPath,
		r.CreatedBy, r.UpdatedBy, syncStatus)

	out := &Record{}
	if err := scanRecord(row, out); err != nil {
		return nil, fmt.Errorf("data: create record: %w", wrapConflict(err))
	}
	return out, nil
}

// GetRecord fetches a record by ID regardless of is_active (callers that
// only want active records should filter, or use ListRecords with
// Active=true). Returns ErrNotFound if no row matches.
func (s *Store) GetRecord(ctx context.Context, id uuid.UUID) (*Record, error) {
	q := `SELECT ` + recordColumns + ` FROM records WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	out := &Record{}
	if err := scanRecord(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get record: %w", err)
	}
	return out, nil
}

// ListRecordsFilter narrows ListRecords; nil fields are not filtered on.
type ListRecordsFilter struct {
	CustomerName *string
	ProjectName  *string
	Active       *bool
}

// ListRecords returns records matching the given filter, most recently
// updated first.
func (s *Store) ListRecords(ctx context.Context, f ListRecordsFilter) ([]*Record, error) {
	q := `SELECT ` + recordColumns + ` FROM records WHERE TRUE`
	var args []any

	if f.CustomerName != nil {
		args = append(args, *f.CustomerName)
		q += fmt.Sprintf(" AND customer_name = $%d", len(args))
	}
	if f.ProjectName != nil {
		args = append(args, *f.ProjectName)
		q += fmt.Sprintf(" AND project_name = $%d", len(args))
	}
	if f.Active != nil {
		args = append(args, *f.Active)
		q += fmt.Sprintf(" AND is_active = $%d", len(args))
	}
	q += " ORDER BY updated_at DESC"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("data: list records: %w", err)
	}
	defer rows.Close()

	var out []*Record
	for rows.Next() {
		r := &Record{}
		if err := scanRecord(rows, r); err != nil {
			return nil, fmt.Errorf("data: list records: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: list records: %w", err)
	}
	return out, nil
}

// UpdateRecord updates a record's mutable fields using optimistic
// concurrency: the WHERE clause requires updated_at to still equal
// expectedUpdatedAt (the value the caller last read, i.e. the OpenAPI
// If-Match header). If zero rows match, the record either doesn't exist
// (ErrNotFound) or was modified since expectedUpdatedAt was read
// (ErrStaleWrite) — the two are disambiguated with a follow-up existence
// check so the API layer can return 404 vs 409 correctly.
func (s *Store) UpdateRecord(ctx context.Context, r *Record, expectedUpdatedAt time.Time) (*Record, error) {
	q := `UPDATE records SET
			customer_name = $1,
			customer_id = $2,
			project_name = $3,
			env_name = $4,
			output_path = $5,
			updated_by = $6,
			updated_at = now(),
			last_commit_sha = $7,
			sync_status = $8
		WHERE id = $9 AND updated_at = $10
		RETURNING ` + recordColumns

	row := s.pool.QueryRow(ctx, q,
		r.CustomerName, r.CustomerID, r.ProjectName, r.EnvName, r.OutputPath,
		r.UpdatedBy, r.LastCommitSHA, r.SyncStatus, r.ID, expectedUpdatedAt)

	out := &Record{}
	if err := scanRecord(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			exists, existsErr := s.recordExists(ctx, r.ID)
			if existsErr != nil {
				return nil, fmt.Errorf("data: update record: %w", existsErr)
			}
			if !exists {
				return nil, ErrNotFound
			}
			return nil, ErrStaleWrite
		}
		return nil, fmt.Errorf("data: update record: %w", wrapConflict(err))
	}
	return out, nil
}

// UpdateRecordSyncStatus is a narrow update used by the output-repo commit
// step (WS-E/WS-G) and the stuck-pending reconciliation job, which don't
// go through the full optimistic-concurrency edit path since they aren't
// user-initiated edits.
func (s *Store) UpdateRecordSyncStatus(ctx context.Context, id uuid.UUID, syncStatus string, lastCommitSHA *string) (*Record, error) {
	q := `UPDATE records SET sync_status = $1, last_commit_sha = COALESCE($2, last_commit_sha), updated_at = updated_at
		WHERE id = $3
		RETURNING ` + recordColumns
	row := s.pool.QueryRow(ctx, q, syncStatus, lastCommitSHA, id)
	out := &Record{}
	if err := scanRecord(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: update record sync status: %w", err)
	}
	return out, nil
}

// SoftDeleteRecord flips is_active to false. Output repo files are
// intentionally left untouched (locked decision) — this function only
// ever touches the DB row.
func (s *Store) SoftDeleteRecord(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE records SET is_active = false, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("data: soft delete record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) recordExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM records WHERE id = $1)`, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("data: record exists: %w", err)
	}
	return exists, nil
}
