package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func scanModuleSchema(row rowScanner, m *ModuleSchema) error {
	return row.Scan(&m.ID, &m.ModuleName, &m.VersionTag, &m.Metadata, &m.Variables, &m.ParsedAt, &m.IsLatest)
}

// UpsertModuleSchema inserts or refreshes the cached schema for one
// (module_name, version_tag) pair. When ms.IsLatest is true, it first
// clears is_latest on that module's other rows inside the same
// transaction, so exactly one row per module_name stays flagged latest —
// mirroring the catalog's "is_latest flips to false" edge case when a
// module is renamed/deleted upstream or a newer tag supersedes it.
func (s *Store) UpsertModuleSchema(ctx context.Context, ms *ModuleSchema) (*ModuleSchema, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("data: upsert module schema: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if ms.IsLatest {
		if _, err := tx.Exec(ctx,
			`UPDATE module_schemas SET is_latest = false WHERE module_name = $1 AND is_latest = true AND version_tag <> $2`,
			ms.ModuleName, ms.VersionTag); err != nil {
			return nil, fmt.Errorf("data: upsert module schema: clear previous latest: %w", err)
		}
	}

	q := `INSERT INTO module_schemas (module_name, version_tag, metadata, variables, is_latest)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (module_name, version_tag) DO UPDATE SET
			metadata = EXCLUDED.metadata,
			variables = EXCLUDED.variables,
			is_latest = EXCLUDED.is_latest,
			parsed_at = now()
		RETURNING id, module_name, version_tag, metadata, variables, parsed_at, is_latest`
	row := tx.QueryRow(ctx, q, ms.ModuleName, ms.VersionTag, ms.Metadata, ms.Variables, ms.IsLatest)
	out := &ModuleSchema{}
	if err := scanModuleSchema(row, out); err != nil {
		return nil, fmt.Errorf("data: upsert module schema: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("data: upsert module schema: commit: %w", err)
	}
	return out, nil
}

// GetLatestModuleSchema returns the row currently flagged is_latest for a
// module. Returns ErrNotFound if the module has no cataloged (tagged)
// schema at all.
func (s *Store) GetLatestModuleSchema(ctx context.Context, moduleName string) (*ModuleSchema, error) {
	q := `SELECT id, module_name, version_tag, metadata, variables, parsed_at, is_latest
		FROM module_schemas WHERE module_name = $1 AND is_latest = true`
	row := s.pool.QueryRow(ctx, q, moduleName)
	out := &ModuleSchema{}
	if err := scanModuleSchema(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get latest module schema: %w", err)
	}
	return out, nil
}

// GetModuleSchemaByVersionTag returns the exact pinned schema for a module
// at a specific version tag (e.g. "vm-instance/v1.2.0"), regardless of
// whether it's still the latest — this is how an existing record's edit
// flow resolves its originally-pinned schema even after upstream drift.
func (s *Store) GetModuleSchemaByVersionTag(ctx context.Context, moduleName, versionTag string) (*ModuleSchema, error) {
	q := `SELECT id, module_name, version_tag, metadata, variables, parsed_at, is_latest
		FROM module_schemas WHERE module_name = $1 AND version_tag = $2`
	row := s.pool.QueryRow(ctx, q, moduleName, versionTag)
	out := &ModuleSchema{}
	if err := scanModuleSchema(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get module schema by version tag: %w", err)
	}
	return out, nil
}
