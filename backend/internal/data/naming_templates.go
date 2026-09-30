package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const namingTemplateColumns = `id, cloud, resource_type, template, num_id_width, created_by, updated_at`

func scanNamingTemplate(row rowScanner, t *NamingTemplate) error {
	return row.Scan(&t.ID, &t.Cloud, &t.ResourceType, &t.Template, &t.NumIDWidth, &t.CreatedBy, &t.UpdatedAt)
}

// UpsertNamingTemplate creates or replaces the template for a (cloud,
// resource_type) pair, matching the OpenAPI POST /admin/naming-templates
// semantics ("create or update"). Token validation (rejecting unknown
// tokens like {typo_token}) is the API layer's responsibility at save
// time, per plan/ARCHITECTURE.md's naming-convention edge cases — this
// method only persists.
func (s *Store) UpsertNamingTemplate(ctx context.Context, t *NamingTemplate) (*NamingTemplate, error) {
	q := `INSERT INTO naming_templates (cloud, resource_type, template, num_id_width, created_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (cloud, resource_type) DO UPDATE SET
			template = EXCLUDED.template,
			num_id_width = EXCLUDED.num_id_width,
			updated_at = now()
		RETURNING ` + namingTemplateColumns
	row := s.pool.QueryRow(ctx, q, t.Cloud, t.ResourceType, t.Template, t.NumIDWidth, t.CreatedBy)
	out := &NamingTemplate{}
	if err := scanNamingTemplate(row, out); err != nil {
		return nil, fmt.Errorf("data: upsert naming template: %w", wrapConflict(err))
	}
	return out, nil
}

// GetNamingTemplate looks up the template for a (cloud, resource_type)
// pair — the primary lookup used by the Naming Convention Engine (WS-I)
// when a module's metadata.yaml declares a naming_resource_type. Returns
// ErrNotFound if no template is configured, which the caller surfaces as
// "no naming template configured for this resource type" per the edge
// case table.
func (s *Store) GetNamingTemplate(ctx context.Context, cloud, resourceType string) (*NamingTemplate, error) {
	q := `SELECT ` + namingTemplateColumns + ` FROM naming_templates WHERE cloud = $1 AND resource_type = $2`
	row := s.pool.QueryRow(ctx, q, cloud, resourceType)
	out := &NamingTemplate{}
	if err := scanNamingTemplate(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get naming template: %w", err)
	}
	return out, nil
}

// ListNamingTemplates returns every configured template, ordered by cloud
// then resource_type.
func (s *Store) ListNamingTemplates(ctx context.Context) ([]*NamingTemplate, error) {
	q := `SELECT ` + namingTemplateColumns + ` FROM naming_templates ORDER BY cloud, resource_type`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("data: list naming templates: %w", err)
	}
	defer rows.Close()

	var out []*NamingTemplate
	for rows.Next() {
		t := &NamingTemplate{}
		if err := scanNamingTemplate(rows, t); err != nil {
			return nil, fmt.Errorf("data: list naming templates: scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: list naming templates: %w", err)
	}
	return out, nil
}

// DeleteNamingTemplate removes a template by ID.
func (s *Store) DeleteNamingTemplate(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM naming_templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("data: delete naming template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
