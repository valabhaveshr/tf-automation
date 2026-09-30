package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const moduleSelectionColumns = `id, record_id, module_name, pinned_version, module_schema_id,
	instances, created_at, updated_at`

func scanModuleSelection(row rowScanner, m *ModuleSelection) error {
	return row.Scan(&m.ID, &m.RecordID, &m.ModuleName, &m.PinnedVersion, &m.ModuleSchemaID,
		&m.Instances, &m.CreatedAt, &m.UpdatedAt)
}

// CreateModuleSelection inserts one module's selection (instance map) under
// a record. A (record_id, module_name) UNIQUE violation is returned as a
// *ConflictError — a record can only select a given module type once (with
// however many instances inside its instance map).
func (s *Store) CreateModuleSelection(ctx context.Context, m *ModuleSelection) (*ModuleSelection, error) {
	q := `INSERT INTO module_selections (record_id, module_name, pinned_version, module_schema_id, instances)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + moduleSelectionColumns
	row := s.pool.QueryRow(ctx, q, m.RecordID, m.ModuleName, m.PinnedVersion, m.ModuleSchemaID, m.Instances)
	out := &ModuleSelection{}
	if err := scanModuleSelection(row, out); err != nil {
		return nil, fmt.Errorf("data: create module selection: %w", wrapConflict(err))
	}
	return out, nil
}

// GetModuleSelection fetches one module selection by its own ID.
func (s *Store) GetModuleSelection(ctx context.Context, id uuid.UUID) (*ModuleSelection, error) {
	q := `SELECT ` + moduleSelectionColumns + ` FROM module_selections WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	out := &ModuleSelection{}
	if err := scanModuleSelection(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get module selection: %w", err)
	}
	return out, nil
}

// ListModuleSelectionsByRecord returns every module selection belonging to
// a record, ordered by module_name for stable rendering.
func (s *Store) ListModuleSelectionsByRecord(ctx context.Context, recordID uuid.UUID) ([]*ModuleSelection, error) {
	q := `SELECT ` + moduleSelectionColumns + ` FROM module_selections WHERE record_id = $1 ORDER BY module_name`
	rows, err := s.pool.Query(ctx, q, recordID)
	if err != nil {
		return nil, fmt.Errorf("data: list module selections: %w", err)
	}
	defer rows.Close()

	var out []*ModuleSelection
	for rows.Next() {
		m := &ModuleSelection{}
		if err := scanModuleSelection(rows, m); err != nil {
			return nil, fmt.Errorf("data: list module selections: scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: list module selections: %w", err)
	}
	return out, nil
}

// UpdateModuleSelection replaces a module selection's pinned version,
// resolved schema, and instance map (e.g. on record edit, or "update to
// latest module version").
func (s *Store) UpdateModuleSelection(ctx context.Context, m *ModuleSelection) (*ModuleSelection, error) {
	q := `UPDATE module_selections SET
			pinned_version = $1,
			module_schema_id = $2,
			instances = $3,
			updated_at = now()
		WHERE id = $4
		RETURNING ` + moduleSelectionColumns
	row := s.pool.QueryRow(ctx, q, m.PinnedVersion, m.ModuleSchemaID, m.Instances, m.ID)
	out := &ModuleSelection{}
	if err := scanModuleSelection(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: update module selection: %w", wrapConflict(err))
	}
	return out, nil
}

// DeleteModuleSelection removes one module selection by ID (hard delete —
// module_selections has no soft-delete concept of its own; it lives and
// dies with its parent record's edit lifecycle).
func (s *Store) DeleteModuleSelection(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM module_selections WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("data: delete module selection: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteModuleSelectionsByRecord removes every module selection under a
// record. Useful for a "replace all selections" edit flow (delete then
// re-create) driven by the API layer inside its own transaction; also
// happens automatically via ON DELETE CASCADE if the parent record is
// hard-deleted (records are normally only soft-deleted, so this is rarely
// triggered that way in practice).
func (s *Store) DeleteModuleSelectionsByRecord(ctx context.Context, recordID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM module_selections WHERE record_id = $1`, recordID); err != nil {
		return fmt.Errorf("data: delete module selections by record: %w", err)
	}
	return nil
}
