package output

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v63/github"
)

// Client wraps a *github.Client for committing generated Terraform code to the
// output repo (WS-E). It deliberately does not know or care how its underlying
// *http.Client/http.RoundTripper is authenticated — GitHub App installation token
// vs. plain PAT is a config/wiring concern owned by WS-G (see
// plan/ARCHITECTURE.md § Locked Decisions: "GitHub credentials: GitHub App").
// Callers construct an authenticated go-github client (or RoundTripper) themselves
// and hand it to NewClient / NewClientFromRoundTripper.
type Client struct {
	gh *github.Client
}

// NewClient builds a Client from an already-authenticated *github.Client. This is
// the preferred constructor: callers that already build a *github.Client (e.g. via
// go-github's own oauth2-backed transport, or a GitHub App installation transport
// such as ghinstallation) can just pass it straight through.
func NewClient(gh *github.Client) *Client {
	return &Client{gh: gh}
}

// NewClientFromRoundTripper builds a Client from an authenticated http.RoundTripper
// (e.g. golang.org/x/oauth2's oauth2.Transport for a PAT, or a GitHub App
// installation-token transport). This is a convenience for callers that have an
// auth-wired RoundTripper but haven't already constructed a *github.Client.
//
// Example (PAT via oauth2, wired by WS-G — this package does not import oauth2
// itself to keep auth mechanics out of this package):
//
//	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: pat})
//	rt := oauth2.NewClient(ctx, ts).Transport
//	client := output.NewClientFromRoundTripper(rt)
func NewClientFromRoundTripper(rt http.RoundTripper) *Client {
	httpClient := &http.Client{Transport: rt}
	return &Client{gh: github.NewClient(httpClient)}
}

// NewEnterpriseClient is like NewClient but targets a GitHub Enterprise Server
// instance instead of github.com. baseURL/uploadURL follow go-github's
// NewEnterpriseClient conventions (typically "https://<host>/api/v3/" and
// "https://<host>/api/uploads/").
func NewEnterpriseClient(rt http.RoundTripper, baseURL, uploadURL string) (*Client, error) {
	httpClient := &http.Client{Transport: rt}
	gh, err := github.NewClient(httpClient).WithEnterpriseURLs(baseURL, uploadURL)
	if err != nil {
		return nil, fmt.Errorf("output: constructing enterprise client: %w", err)
	}
	return &Client{gh: gh}, nil
}

// Underlying exposes the wrapped *github.Client for advanced callers (e.g. WS-G
// wanting to inspect rate-limit headers directly). Most callers should not need this.
func (c *Client) Underlying() *github.Client {
	return c.gh
}

// GetFile fetches the current content and blob SHA of a single file in the output
// repo at the given path (repo-relative, e.g. "acme/webapp/prod/main.tf"), read at
// the repo's default branch HEAD. It returns ErrFileNotFound (wrapped) if the path
// does not exist.
//
// This is used internally by CommitFiles (see commit.go) to establish the current
// branch state, and is also exported for callers (WS-G) that want to display
// current output-repo content, e.g. for a "what's currently committed" view.
func (c *Client) GetFile(ctx context.Context, owner, repo, path string) (content []byte, sha string, err error) {
	var fileContent *github.RepositoryContent

	retryErr := withRateLimitRetry(ctx, func() (*github.Response, error) {
		fc, dirContent, resp, gerr := c.gh.Repositories.GetContents(ctx, owner, repo, path, nil)
		if gerr != nil {
			return resp, gerr
		}
		if fc == nil {
			if len(dirContent) > 0 {
				return resp, fmt.Errorf("output: %q is a directory, not a file", path)
			}
			return resp, fmt.Errorf("output: unexpected empty response for %q", path)
		}
		fileContent = fc
		return resp, nil
	})

	if retryErr != nil {
		if isNotFound(retryErr) {
			return nil, "", fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		return nil, "", fmt.Errorf("output: getting %q: %w", path, retryErr)
	}

	decoded, err := fileContent.GetContent()
	if err != nil {
		return nil, "", fmt.Errorf("output: decoding content for %q: %w", path, err)
	}

	return []byte(decoded), fileContent.GetSHA(), nil
}

// isNotFound reports whether err represents a GitHub 404 response, whether raw or
// wrapped in one of go-github's typed error types.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	ge, ok := asErrorResponse(err)
	if !ok {
		return false
	}
	return ge.Response != nil && ge.Response.StatusCode == http.StatusNotFound
}

func asErrorResponse(err error) (*github.ErrorResponse, bool) {
	er, ok := err.(*github.ErrorResponse)
	if ok {
		return er, true
	}
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return asErrorResponse(u.Unwrap())
	}
	return nil, false
}
