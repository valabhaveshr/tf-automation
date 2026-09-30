package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"tf-automation/backend/internal/data"
)

// ---------------------------------------------------------------------
// Transactional record + module_selections persistence.
//
// WS-A's Store.CreateRecord/UpdateRecord/CreateModuleSelection/
// DeleteModuleSelectionsByRecord are each bound directly to *Store's own
// pgxpool.Pool (not to an injectable executor/transaction), so they cannot
// be composed into one externally-managed transaction as-is. Rather than
// modify those WS-A files (out of this workstream's scope) or add a
// transaction-scoped variant of every Store method it would take to cover
// this one composite operation, this file duplicates the exact same SQL
// text WS-A's records.go/module_selections.go use, scoped to a
// caller-supplied pgx.Tx, using Store.Pool() -- which store.go's own doc
// comment explicitly names as the intended extension point for exactly
// this: "callers that need to run raw queries or compose a multi-table
// transaction spanning operations this package doesn't otherwise provide
// (e.g. WS-G's save transaction that allocates a naming counter and
// inserts a record together)."
//
// IMPORTANT scope note (see the task report for the full rationale): this
// gives the record row + its module_selections rows true, single-
// transaction atomicity. It does NOT extend to naming.Engine.Allocate's
// counter allocation, which runs in its own, separately-committed
// transaction inside WS-A/WS-I (AllocateNamingCounter begins+commits its
// own tx per call, and naming.Engine has no tx-injectable variant). That
// gap is accepted per the task brief's documented fallback: if the record
// insert below fails after a counter was already allocated, the counter
// value is burned and NOT rolled back. This is logged wherever it can
// happen (records.go).
// ---------------------------------------------------------------------

const pgUniqueViolationCode = "23505"

// wrapConflictLocal mirrors data package's unexported wrapConflict, which
// this package cannot call directly.
func wrapConflictLocal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode {
		return &data.ConflictError{Constraint: pgErr.ConstraintName, Detail: pgErr.Message}
	}
	return err
}

const recordColumnsLocal = `id, customer_name, customer_id, project_name, env_name, output_path,
	is_active, created_by, created_at, updated_by, updated_at, last_commit_sha, sync_status`

func scanRecordRow(row pgx.Row, r *data.Record) error {
	return row.Scan(&r.ID, &r.CustomerName, &r.CustomerID, &r.ProjectName, &r.EnvName, &r.OutputPath,
		&r.IsActive, &r.CreatedBy, &r.CreatedAt, &r.UpdatedBy, &r.UpdatedAt, &r.LastCommitSHA, &r.SyncStatus)
}

// insertRecordRow mirrors data.Store.CreateRecord's SQL exactly, scoped to
// tx.
func insertRecordRow(ctx context.Context, tx pgx.Tx, r *data.Record) (*data.Record, error) {
	syncStatus := r.SyncStatus
	if syncStatus == "" {
		syncStatus = data.SyncStatusPending
	}
	q := `INSERT INTO records (customer_name, customer_id, project_name, env_name, output_path,
			created_by, updated_by, sync_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + recordColumnsLocal

	row := tx.QueryRow(ctx, q, r.CustomerName, r.CustomerID, r.ProjectName, r.EnvName, r.OutputPath,
		r.CreatedBy, r.UpdatedBy, syncStatus)
	out := &data.Record{}
	if err := scanRecordRow(row, out); err != nil {
		return nil, fmt.Errorf("api: insert record: %w", wrapConflictLocal(err))
	}
	return out, nil
}

// execUpdateRecordRow mirrors data.Store.UpdateRecord's exact optimistic-
// concurrency SQL (WHERE id = $X AND updated_at = $Y, disambiguating
// not-found vs stale-write with the same follow-up existence check),
// scoped to tx.
func execUpdateRecordRow(ctx context.Context, tx pgx.Tx, r *data.Record, expectedUpdatedAt time.Time) (*data.Record, error) {
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
		RETURNING ` + recordColumnsLocal

	row := tx.QueryRow(ctx, q, r.CustomerName, r.CustomerID, r.ProjectName, r.EnvName, r.OutputPath,
		r.UpdatedBy, r.LastCommitSHA, r.SyncStatus, r.ID, expectedUpdatedAt)
	out := &data.Record{}
	if err := scanRecordRow(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			existsErr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM records WHERE id = $1)`, r.ID).Scan(&exists)
			if existsErr != nil {
				return nil, fmt.Errorf("api: update record: existence check: %w", existsErr)
			}
			if !exists {
				return nil, data.ErrNotFound
			}
			return nil, data.ErrStaleWrite
		}
		return nil, fmt.Errorf("api: update record: %w", wrapConflictLocal(err))
	}
	return out, nil
}

const moduleSelectionColumnsLocal = `id, record_id, module_name, pinned_version, module_schema_id,
	instances, created_at, updated_at`

func scanModuleSelectionRow(row pgx.Row, m *data.ModuleSelection) error {
	return row.Scan(&m.ID, &m.RecordID, &m.ModuleName, &m.PinnedVersion, &m.ModuleSchemaID,
		&m.Instances, &m.CreatedAt, &m.UpdatedAt)
}

// insertModuleSelectionRow mirrors data.Store.CreateModuleSelection's SQL
// exactly, scoped to tx.
func insertModuleSelectionRow(ctx context.Context, tx pgx.Tx, m *data.ModuleSelection) (*data.ModuleSelection, error) {
	q := `INSERT INTO module_selections (record_id, module_name, pinned_version, module_schema_id, instances)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + moduleSelectionColumnsLocal
	row := tx.QueryRow(ctx, q, m.RecordID, m.ModuleName, m.PinnedVersion, m.ModuleSchemaID, m.Instances)
	out := &data.ModuleSelection{}
	if err := scanModuleSelectionRow(row, out); err != nil {
		return nil, fmt.Errorf("api: insert module selection: %w", wrapConflictLocal(err))
	}
	return out, nil
}

// deleteModuleSelectionsRow mirrors
// data.Store.DeleteModuleSelectionsByRecord's SQL exactly, scoped to tx.
func deleteModuleSelectionsRow(ctx context.Context, tx pgx.Tx, recordID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM module_selections WHERE record_id = $1`, recordID); err != nil {
		return fmt.Errorf("api: delete module selections: %w", err)
	}
	return nil
}

// createRecordTx inserts rec and every resolved module's selection row in
// one transaction: if any INSERT fails (including a UNIQUE violation on
// (customer_name, project_name, env_name), mapped to 409 by writeErr via
// *data.ConflictError), the whole transaction rolls back and nothing is
// persisted.
func (s *Server) createRecordTx(ctx context.Context, rec *data.Record, resolved []*resolvedModule) (*data.Record, []*data.ModuleSelection, error) {
	tx, err := s.Store.Pool().Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("api: begin create-record tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	savedRecord, err := insertRecordRow(ctx, tx, rec)
	if err != nil {
		return nil, nil, err
	}

	savedSelections := make([]*data.ModuleSelection, 0, len(resolved))
	for _, rm := range resolved {
		instancesJSON, err := json.Marshal(rm.input.Instances)
		if err != nil {
			return nil, nil, fmt.Errorf("api: encoding instances for module %q: %w", rm.input.ModuleName, err)
		}
		saved, err := insertModuleSelectionRow(ctx, tx, &data.ModuleSelection{
			RecordID:       savedRecord.ID,
			ModuleName:     rm.input.ModuleName,
			PinnedVersion:  rm.schema.VersionTag,
			ModuleSchemaID: rm.schema.ID,
			Instances:      instancesJSON,
		})
		if err != nil {
			return nil, nil, err
		}
		savedSelections = append(savedSelections, saved)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("api: commit create-record tx: %w", err)
	}
	return savedRecord, savedSelections, nil
}

// updateRecordTx applies the optimistic-concurrency record update and
// replaces every module_selections row (delete-then-recreate, matching the
// pattern data.go's own doc comment describes as the API layer's
// responsibility: "Useful for a 'replace all selections' edit flow (delete
// then re-create) driven by the API layer inside its own transaction") --
// all in one transaction. A stale If-Match (data.ErrStaleWrite) or missing
// record (data.ErrNotFound) is detected before any module_selections write
// happens, and rolls back cleanly either way.
func (s *Server) updateRecordTx(ctx context.Context, rec *data.Record, expectedUpdatedAt time.Time, resolved []*resolvedModule) (*data.Record, []*data.ModuleSelection, error) {
	tx, err := s.Store.Pool().Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("api: begin update-record tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	savedRecord, err := execUpdateRecordRow(ctx, tx, rec, expectedUpdatedAt)
	if err != nil {
		return nil, nil, err
	}

	if err := deleteModuleSelectionsRow(ctx, tx, savedRecord.ID); err != nil {
		return nil, nil, err
	}

	savedSelections := make([]*data.ModuleSelection, 0, len(resolved))
	for _, rm := range resolved {
		instancesJSON, err := json.Marshal(rm.input.Instances)
		if err != nil {
			return nil, nil, fmt.Errorf("api: encoding instances for module %q: %w", rm.input.ModuleName, err)
		}
		saved, err := insertModuleSelectionRow(ctx, tx, &data.ModuleSelection{
			RecordID:       savedRecord.ID,
			ModuleName:     rm.input.ModuleName,
			PinnedVersion:  rm.schema.VersionTag,
			ModuleSchemaID: rm.schema.ID,
			Instances:      instancesJSON,
		})
		if err != nil {
			return nil, nil, err
		}
		savedSelections = append(savedSelections, saved)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("api: commit update-record tx: %w", err)
	}
	return savedRecord, savedSelections, nil
}
