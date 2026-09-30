package output

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v63/github"
)

// CommitFiles writes a batch of generated files to the output repo as a single,
// atomic git commit on the repo's default branch, at paths
// "<basePath>/<filename>" for each entry in files. message becomes the commit
// message (callers, i.e. WS-G, should include the record ID for traceability, per
// the "one commit per record save" mental model in plan/ARCHITECTURE.md).
//
// It returns the new commit SHA on success. This is the value WS-G should persist
// as records.last_commit_sha alongside sync_status=synced, per the transaction-flow
// sequence diagram (§ Phase 1 technical design detail).
//
// Conflict behavior: this reads the branch's current HEAD before building the
// commit, and performs a non-force ref update at the end. If the branch moved
// (anyone — the app or a manual edit — pushed a commit) between the read and the
// write, the update is rejected as a non-fast-forward, and that is surfaced as
// ErrOutputModifiedExternally (wrapped) rather than silently overwritten. Callers
// should errors.Is(err, ErrOutputModifiedExternally) and set
// sync_status=failed with an explicit message, per the "Output repo file was
// manually modified outside the app" edge case in plan/ARCHITECTURE.md.
//
// Rate limiting: individual API calls made along the way are retried with bounded
// backoff on GitHub rate-limit responses (see retry.go); if retries are exhausted,
// the error wraps ErrRateLimited, which is a transient failure a later retry (e.g.
// the sync_status reconciliation job) may resolve.
func (c *Client) CommitFiles(ctx context.Context, owner, repo, basePath string, files map[string][]byte, message string) (commitSHA string, err error) {
	if len(files) == 0 {
		return "", fmt.Errorf("output: CommitFiles called with no files")
	}

	branch, err := c.defaultBranch(ctx, owner, repo)
	if err != nil {
		return "", err
	}

	return c.commitFilesToRef(ctx, owner, repo, "refs/heads/"+branch, basePath, files, message)
}

func (c *Client) defaultBranch(ctx context.Context, owner, repo string) (string, error) {
	var branch string
	err := withRateLimitRetry(ctx, func() (*github.Response, error) {
		r, resp, gerr := c.gh.Repositories.Get(ctx, owner, repo)
		if gerr != nil {
			return resp, gerr
		}
		branch = r.GetDefaultBranch()
		return resp, nil
	})
	if err != nil {
		return "", fmt.Errorf("output: resolving default branch for %s/%s: %w", owner, repo, err)
	}
	if branch == "" {
		branch = "main"
	}
	return branch, nil
}

// commitFilesToRef implements the atomic multi-file commit via the Git Data API:
// create a blob per file, a tree that layers those blobs onto the current base
// tree (so files outside this batch are untouched), a commit pointing at that
// tree, then a non-force ref update as the optimistic-concurrency check.
func (c *Client) commitFilesToRef(ctx context.Context, owner, repo, ref, basePath string, files map[string][]byte, message string) (string, error) {
	// 1. Read current branch HEAD. This is the "last read" state our conflict check
	//    is conditioned on.
	var baseRef *github.Reference
	err := withRateLimitRetry(ctx, func() (*github.Response, error) {
		r, resp, gerr := c.gh.Git.GetRef(ctx, owner, repo, ref)
		if gerr != nil {
			return resp, gerr
		}
		baseRef = r
		return resp, nil
	})
	if err != nil {
		return "", fmt.Errorf("output: reading ref %q: %w", ref, err)
	}
	baseCommitSHA := baseRef.GetObject().GetSHA()

	// 2. Resolve the base commit's tree, used as CreateTree's base_tree so paths not
	//    in this batch are carried forward unchanged.
	var baseCommit *github.Commit
	err = withRateLimitRetry(ctx, func() (*github.Response, error) {
		cm, resp, gerr := c.gh.Git.GetCommit(ctx, owner, repo, baseCommitSHA)
		if gerr != nil {
			return resp, gerr
		}
		baseCommit = cm
		return resp, nil
	})
	if err != nil {
		return "", fmt.Errorf("output: reading base commit %q: %w", baseCommitSHA, err)
	}
	baseTreeSHA := baseCommit.GetTree().GetSHA()

	// 3. Create a blob per file and a corresponding tree entry.
	entries := make([]*github.TreeEntry, 0, len(files))
	for name, content := range files {
		path := joinRepoPath(basePath, name)

		encoded := base64.StdEncoding.EncodeToString(content)
		encoding := "base64"

		var blob *github.Blob
		err := withRateLimitRetry(ctx, func() (*github.Response, error) {
			b, resp, gerr := c.gh.Git.CreateBlob(ctx, owner, repo, &github.Blob{
				Content:  &encoded,
				Encoding: &encoding,
			})
			if gerr != nil {
				return resp, gerr
			}
			blob = b
			return resp, nil
		})
		if err != nil {
			return "", fmt.Errorf("output: creating blob for %q: %w", path, err)
		}

		entries = append(entries, &github.TreeEntry{
			Path: github.String(path),
			Mode: github.String("100644"),
			Type: github.String("blob"),
			SHA:  blob.SHA,
		})
	}

	// 4. Create the tree.
	var tree *github.Tree
	err = withRateLimitRetry(ctx, func() (*github.Response, error) {
		t, resp, gerr := c.gh.Git.CreateTree(ctx, owner, repo, baseTreeSHA, entries)
		if gerr != nil {
			return resp, gerr
		}
		tree = t
		return resp, nil
	})
	if err != nil {
		return "", fmt.Errorf("output: creating tree: %w", err)
	}

	// 5. Create the commit object (not yet reachable from any ref).
	var commit *github.Commit
	err = withRateLimitRetry(ctx, func() (*github.Response, error) {
		cm, resp, gerr := c.gh.Git.CreateCommit(ctx, owner, repo, &github.Commit{
			Message: github.String(message),
			Tree:    tree,
			Parents: []*github.Commit{{SHA: github.String(baseCommitSHA)}},
		}, nil)
		if gerr != nil {
			return resp, gerr
		}
		commit = cm
		return resp, nil
	})
	if err != nil {
		return "", fmt.Errorf("output: creating commit: %w", err)
	}

	// 6. Move the branch ref to the new commit, non-force. This is the atomic
	//    "commit all files together" step, and doubles as the conflict check: if
	//    the ref no longer points at baseCommitSHA (someone committed since step 1),
	//    the new commit is not a fast-forward and GitHub rejects the update.
	var updatedRef *github.Reference
	err = withRateLimitRetry(ctx, func() (*github.Response, error) {
		r, resp, gerr := c.gh.Git.UpdateRef(ctx, owner, repo, &github.Reference{
			Ref:    github.String(ref),
			Object: &github.GitObject{SHA: commit.SHA},
		}, false)
		if gerr != nil {
			return resp, gerr
		}
		updatedRef = r
		return resp, nil
	})
	if err != nil {
		if isConflict(err) {
			return "", newConflictError(fmt.Sprintf(
				"ref %q was at %s when read but moved before the update could land: %v",
				ref, baseCommitSHA, err))
		}
		return "", fmt.Errorf("output: updating ref %q: %w", ref, err)
	}

	return updatedRef.GetObject().GetSHA(), nil
}

func joinRepoPath(basePath, name string) string {
	base := strings.Trim(basePath, "/")
	name = strings.TrimPrefix(name, "/")
	if base == "" {
		return name
	}
	return base + "/" + name
}

// isConflict reports whether err represents a GitHub conflict/unprocessable
// response (409 or 422) — the shape a non-fast-forward ref update, or a stale
// Contents-API SHA, comes back as.
func isConflict(err error) bool {
	ge, ok := asErrorResponse(err)
	if !ok || ge.Response == nil {
		return false
	}
	code := ge.Response.StatusCode
	return code == http.StatusConflict || code == http.StatusUnprocessableEntity
}
