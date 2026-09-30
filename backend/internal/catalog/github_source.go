package catalog

import (
	"context"
	"fmt"

	"github.com/google/go-github/v63/github"
)

// ModulesRepoSource abstracts read access to the modules repo (tags + file
// contents at a ref) that Service needs. Kept as a small interface -- like
// WS-E's output.Client wraps a *github.Client without owning auth -- so
// Service can be unit-tested against a fake in-memory implementation
// (see *_test.go) with no real GitHub calls, and so auth wiring (GitHub App
// vs PAT) stays entirely WS-G's concern.
type ModulesRepoSource interface {
	// ListTags returns every git tag in the modules repo (all pages).
	ListTags(ctx context.Context) ([]string, error)

	// GetFileAtRef fetches a single file's raw contents as they exist at ref
	// (a tag name such as "vm-instance/v1.2.0", or a branch name). Returns a
	// non-nil error if the file is missing, the ref doesn't exist, or the
	// path is a directory -- callers treat any error here as "unreadable"
	// per the "missing variables.tf/metadata.yaml" edge case.
	GetFileAtRef(ctx context.Context, path, ref string) ([]byte, error)
}

// GitHubSource is the concrete ModulesRepoSource backed by the real GitHub
// API via go-github (the same library WS-E already added -- no separate
// git-clone dependency). Callers pass in an already-authenticated
// *github.Client; auth wiring (GitHub App installation token vs PAT) is
// WS-G's job, matching the pattern output.NewClient uses.
type GitHubSource struct {
	gh    *github.Client
	owner string
	repo  string
}

// NewGitHubSource builds a GitHubSource for the modules repo at
// owner/repo, using an already-authenticated *github.Client.
func NewGitHubSource(gh *github.Client, owner, repo string) *GitHubSource {
	return &GitHubSource{gh: gh, owner: owner, repo: repo}
}

// ListTags returns every tag name in the modules repo, paginating through
// all result pages (go-github returns at most one page per call).
func (s *GitHubSource) ListTags(ctx context.Context) ([]string, error) {
	var tags []string
	opt := &github.ListOptions{PerPage: 100}
	for {
		pageTags, resp, err := s.gh.Repositories.ListTags(ctx, s.owner, s.repo, opt)
		if err != nil {
			return nil, fmt.Errorf("catalog: listing tags for %s/%s: %w", s.owner, s.repo, err)
		}
		for _, t := range pageTags {
			tags = append(tags, t.GetName())
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return tags, nil
}

// GetFileAtRef fetches path's raw contents as they exist at ref (a tag or
// branch name), via the Contents API's Ref option.
func (s *GitHubSource) GetFileAtRef(ctx context.Context, path, ref string) ([]byte, error) {
	fileContent, dirContent, _, err := s.gh.Repositories.GetContents(
		ctx, s.owner, s.repo, path, &github.RepositoryContentGetOptions{Ref: ref},
	)
	if err != nil {
		return nil, fmt.Errorf("catalog: getting %q at ref %q: %w", path, ref, err)
	}
	if fileContent == nil {
		if len(dirContent) > 0 {
			return nil, fmt.Errorf("catalog: %q at ref %q is a directory, not a file", path, ref)
		}
		return nil, fmt.Errorf("catalog: unexpected empty response for %q at ref %q", path, ref)
	}
	decoded, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("catalog: decoding content for %q at ref %q: %w", path, ref, err)
	}
	return []byte(decoded), nil
}
