module tf-automation/backend

go 1.22

require (
	github.com/google/go-github/v63 v63.0.0
	golang.org/x/oauth2 v0.24.0
)

require (
	github.com/golang-jwt/jwt/v5 v5.2.1
	golang.org/x/crypto v0.31.0
)

// Added by P3-B (OIDC SSO login) for OIDC discovery, ID-token verification,
// and the authorization-code exchange (backend/internal/auth/oidc.go).
// golang.org/x/oauth2 was already present (used elsewhere); go-oidc/v3 is
// new. NOTE: go.sum was NOT regenerated (no Go toolchain available in this
// environment) -- run `go mod tidy` once Go is installed. That will also
// pull in go-oidc's own indirect deps (e.g. github.com/go-jose/go-jose/v4)
// which aren't listed here.
require github.com/coreos/go-oidc/v3 v3.11.0

require (
	github.com/google/uuid v1.6.0
	github.com/hashicorp/hcl/v2 v2.21.0
	github.com/jackc/pgx/v5 v5.6.0
	github.com/zclconf/go-cty v1.14.4
)

require gopkg.in/yaml.v3 v3.0.1

// Added by WS-G (backend API orchestration) for HTTP routing (path params +
// middleware chaining). No other workstream had added a router library yet.
// NOTE: go.sum was NOT regenerated (no Go toolchain available in this
// environment) -- run `go mod tidy` once Go is installed to fetch the
// checksum entries for this module before building.
require github.com/go-chi/chi/v5 v5.1.0
