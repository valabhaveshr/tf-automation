package naming

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"tf-automation/backend/internal/data"
)

// businessShortCodePattern enforces the customers.business_short_code
// format documented alongside plan/ARCHITECTURE.md's data model SQL:
// "<=5 chars, validated format" — lowercase alphanumeric, 1 to 5 chars.
var businessShortCodePattern = regexp.MustCompile(`^[a-z0-9]{1,5}$`)

// NormalizeBusinessShortCode trims and lowercases a user-entered short name
// ("DMSC" -> "dmsc") and validates it against the same rule CreateCustomer
// enforces. Short codes are stored lowercase because they're substituted
// into resource names, which GCP requires to be lowercase.
func NormalizeBusinessShortCode(raw string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(raw))
	if !businessShortCodePattern.MatchString(code) {
		return "", fmt.Errorf("short name must be 1-5 letters or digits, e.g. DMSC")
	}
	return code, nil
}

// CreateCustomer validates businessShortCode before delegating to WS-A's
// customers repo (Store.CreateCustomer). This is the thin validation
// layer WS-G's admin POST /api/admin/customers handler calls — real
// persistence (including the UNIQUE(name) conflict translation) stays in
// WS-A.
func (e *Engine) CreateCustomer(ctx context.Context, name, businessShortCode string) (*data.Customer, error) {
	if !businessShortCodePattern.MatchString(businessShortCode) {
		return nil, fmt.Errorf("naming: invalid business_short_code %q: must be 1-5 lowercase alphanumeric characters", businessShortCode)
	}
	return e.store.CreateCustomer(ctx, &data.Customer{
		Name:              name,
		BusinessShortCode: businessShortCode,
	})
}

// SaveNamingTemplate validates tmpl.Template's tokens (ValidateTemplate)
// before delegating to WS-A's naming_templates repo
// (Store.UpsertNamingTemplate). This is the thin validation layer WS-G's
// admin POST /api/admin/naming-templates handler calls — per the locked
// decision, an unknown token must be rejected here, at save time, not
// later at generation time.
func (e *Engine) SaveNamingTemplate(ctx context.Context, tmpl data.NamingTemplate) (*data.NamingTemplate, error) {
	if err := ValidateTemplate(tmpl.Template); err != nil {
		return nil, err
	}
	return e.store.UpsertNamingTemplate(ctx, &tmpl)
}
