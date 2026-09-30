// Package naming is WS-I: the Naming Convention Engine described in
// plan/ARCHITECTURE.md's "Naming convention" locked decision and the
// "Naming convention" edge-case table. It resolves an admin-managed
// naming_templates row (WS-A) plus a customer's business_short_code
// (WS-A) into a computed, editable resource name — either as a tentative,
// non-allocating preview (Suggest) or as the real save-time allocation
// (Allocate), which is the only path that ever consumes a num_id counter
// slot.
package naming

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// allowedTokens is the exact set of naming-template tokens permitted by
// plan/ARCHITECTURE.md § Phase 1 -> Data model, "Available naming
// template tokens": {business_short_code}, {env}, {project},
// {user_label}, {num_id}. A resource-type prefix (prj-, fldr-, vpc-, ...)
// is written directly into the template string, not templated as a
// token, so it is intentionally absent from this set.
var allowedTokens = map[string]bool{
	"business_short_code": true,
	"env":                 true,
	"project":             true,
	"user_label":          true,
	"num_id":              true,
}

// tokenPattern matches {token_name} placeholders in a naming template
// string.
var tokenPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

// ValidateTemplate rejects any {token} placeholder in template that isn't
// one of allowedTokens. Per plan/ARCHITECTURE.md's naming-convention edge
// case ("Template references an unknown token... Rejected at template
// save time"), this MUST be called at template SAVE time, not at
// generation time. WS-G's admin naming-template save endpoint calls this
// (directly, or via SaveNamingTemplate in admin.go) before persisting via
// WS-A's naming_templates repo.
func ValidateTemplate(template string) error {
	matches := tokenPattern.FindAllStringSubmatch(template, -1)

	seen := map[string]bool{}
	var unknown []string
	for _, m := range matches {
		token := m[1]
		if !allowedTokens[token] && !seen[token] {
			unknown = append(unknown, token)
			seen[token] = true
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("naming: template references unknown token(s): {%s}", strings.Join(unknown, "}, {"))
	}
	return nil
}

// RenderTemplate substitutes every {token} placeholder in template with
// its resolved value. {business_short_code}, {env}, {project}, and
// {user_label} are looked up in values; {num_id} is formatted as numID
// zero-padded to numIDWidth (e.g. width=2 -> "01", "02", ..., "99";
// width=4 -> "0001", ...).
//
// Returns an error if a referenced token has no supplied value. This
// should never happen for a template that passed ValidateTemplate at save
// time (every token it allows is either produced here from values or from
// numID), but RenderTemplate defends against it anyway since it runs on
// every preview and save, not just at admin save time.
func RenderTemplate(template string, values map[string]string, numID int, numIDWidth int) (string, error) {
	var renderErr error
	result := tokenPattern.ReplaceAllStringFunc(template, func(match string) string {
		if renderErr != nil {
			return match
		}
		token := tokenPattern.FindStringSubmatch(match)[1]

		if token == "num_id" {
			return fmt.Sprintf("%0*d", numIDWidth, numID)
		}

		val, ok := values[token]
		if !ok {
			renderErr = fmt.Errorf("naming: render template: no value supplied for token %q", token)
			return match
		}
		return val
	})
	if renderErr != nil {
		return "", renderErr
	}
	return result, nil
}
