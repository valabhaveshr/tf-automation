package output

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v63/github"
)

// --- test scaffolding -------------------------------------------------------

// newTestClient wires a *Client whose underlying *github.Client points at a local
// httptest server instead of the real GitHub API, following the same pattern
// go-github's own test suite uses (override BaseURL/UploadURL on a client built
// with a nil http.Client).
func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	gh := github.NewClient(nil)
	base, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}
	gh.BaseURL = base
	gh.UploadURL = base
	return NewClient(gh)
}

// writeJSON and decodeBody run inside the httptest server's per-request
// goroutines, not the test's own goroutine, so they must use t.Errorf (safe to
// call from any goroutine) rather than t.Fatalf/t.Fatal, which the testing
// package requires to be called only from the goroutine running the test
// function itself.
func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encoding test response: %v", err)
	}
}

func decodeBody(t *testing.T, r *http.Request, v any) bool {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Errorf("decoding request body on %s: %v", r.URL.Path, err)
		return false
	}
	return true
}

// wire-format structs matching what go-github's Git Data API client actually
// sends, independent of go-github's own (private) request types.
type blobRequestBody struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type treeEntryBody struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type treeRequestBody struct {
	BaseTree string          `json:"base_tree"`
	Tree     []treeEntryBody `json:"tree"`
}

type commitRequestBody struct {
	Message string   `json:"message"`
	Tree    string   `json:"tree"`
	Parents []string `json:"parents"`
}

type updateRefRequestBody struct {
	SHA   string `json:"sha"`
	Force bool   `json:"force"`
}

// --- successful multi-file atomic commit ------------------------------------

func TestCommitFiles_MultiFileAtomicCommit(t *testing.T) {
	const owner, repo = "acme", "output-repo"
	basePath := "acme/proj/prod"
	files := map[string][]byte{
		"main.tf": []byte("# main content"),
		"vars.tf": []byte("# vars content"),
	}
	wantPaths := map[string][]byte{
		basePath + "/main.tf": files["main.tf"],
		basePath + "/vars.tf": files["vars.tf"],
	}

	var mu sync.Mutex
	blobSHAByContent := map[string]string{} // raw content -> fake blob sha
	blobContentBySHA := map[string]string{} // fake blob sha -> raw content

	mux := http.NewServeMux()

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s on repo endpoint", r.Method)
		}
		writeJSON(t, w, http.StatusOK, github.Repository{
			DefaultBranch: github.String("main"),
		})
	})

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/ref/heads/main", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.Reference{
			Ref: github.String("refs/heads/main"),
			Object: &github.GitObject{
				Type: github.String("commit"),
				SHA:  github.String("base123"),
			},
		})
	})

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/commits/base123", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.Commit{
			SHA:  github.String("base123"),
			Tree: &github.Tree{SHA: github.String("basetree1")},
		})
	})

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/blobs", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		var body blobRequestBody
		if !decodeBody(t, r, &body) {
			return
		}
		if body.Encoding != "base64" {
			t.Errorf("expected base64 blob encoding, got %q", body.Encoding)
			return
		}
		raw, err := base64.StdEncoding.DecodeString(body.Content)
		if err != nil {
			t.Errorf("decoding blob content: %v", err)
			return
		}

		mu.Lock()
		sha, ok := blobSHAByContent[string(raw)]
		if !ok {
			sha = fmt.Sprintf("blobsha-%d", len(blobSHAByContent)+1)
			blobSHAByContent[string(raw)] = sha
			blobContentBySHA[sha] = string(raw)
		}
		mu.Unlock()

		writeJSON(t, w, http.StatusCreated, github.Blob{SHA: github.String(sha)})
	})

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/trees", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		var body treeRequestBody
		if !decodeBody(t, r, &body) {
			return
		}

		if body.BaseTree != "basetree1" {
			t.Errorf("tree request base_tree = %q, want %q", body.BaseTree, "basetree1")
		}
		if len(body.Tree) != len(wantPaths) {
			t.Errorf("tree request has %d entries, want %d", len(body.Tree), len(wantPaths))
			writeJSON(t, w, http.StatusCreated, github.Tree{SHA: github.String("newtree1")})
			return
		}

		mu.Lock()
		for _, e := range body.Tree {
			wantContent, ok := wantPaths[e.Path]
			if !ok {
				t.Errorf("unexpected tree entry path %q", e.Path)
				continue
			}
			gotContent, ok := blobContentBySHA[e.SHA]
			if !ok || gotContent != string(wantContent) {
				t.Errorf("tree entry for %q has sha %q whose blob content = %q, want %q", e.Path, e.SHA, gotContent, string(wantContent))
			}
			if e.Mode != "100644" || e.Type != "blob" {
				t.Errorf("tree entry for %q has mode/type %q/%q, want 100644/blob", e.Path, e.Mode, e.Type)
			}
		}
		mu.Unlock()

		writeJSON(t, w, http.StatusCreated, github.Tree{SHA: github.String("newtree1")})
	})

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/commits", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s on git/commits", r.Method)
		}
		var body commitRequestBody
		decodeBody(t, r, &body)
		if body.Tree != "newtree1" {
			t.Errorf("commit request tree = %q, want %q", body.Tree, "newtree1")
		}
		if len(body.Parents) != 1 || body.Parents[0] != "base123" {
			t.Errorf("commit request parents = %v, want [base123]", body.Parents)
		}
		if body.Message != "test commit" {
			t.Errorf("commit request message = %q, want %q", body.Message, "test commit")
		}
		writeJSON(t, w, http.StatusCreated, github.Commit{SHA: github.String("commit456")})
	})

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/refs/heads/main", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s on git/refs/heads/main", r.Method)
		}
		var body updateRefRequestBody
		decodeBody(t, r, &body)
		if body.SHA != "commit456" {
			t.Errorf("update-ref sha = %q, want %q", body.SHA, "commit456")
		}
		if body.Force {
			t.Errorf("update-ref force = true, want false (non-fast-forward should be rejected, not forced)")
		}
		writeJSON(t, w, http.StatusOK, github.Reference{
			Ref:    github.String("refs/heads/main"),
			Object: &github.GitObject{Type: github.String("commit"), SHA: github.String("commit456")},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)

	sha, err := client.CommitFiles(context.Background(), owner, repo, basePath, files, "test commit")
	if err != nil {
		t.Fatalf("CommitFiles returned error: %v", err)
	}
	if sha != "commit456" {
		t.Errorf("CommitFiles sha = %q, want %q", sha, "commit456")
	}
}

// --- conditional-update conflict: output modified externally ----------------

func TestCommitFiles_ConflictReturnsErrOutputModifiedExternally(t *testing.T) {
	const owner, repo = "acme", "output-repo"
	basePath := "acme/proj/prod"
	files := map[string][]byte{"main.tf": []byte("content")}

	mux := http.NewServeMux()

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.Repository{DefaultBranch: github.String("main")})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/ref/heads/main", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.Reference{
			Ref:    github.String("refs/heads/main"),
			Object: &github.GitObject{Type: github.String("commit"), SHA: github.String("base123")},
		})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/commits/base123", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.Commit{SHA: github.String("base123"), Tree: &github.Tree{SHA: github.String("basetree1")}})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/blobs", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, github.Blob{SHA: github.String("blobsha-1")})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/trees", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, github.Tree{SHA: github.String("newtree1")})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/commits", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, github.Commit{SHA: github.String("commit456")})
	})
	// Simulate someone having pushed to the branch out from under us: reject the
	// non-force ref update as a non-fast-forward, the way real GitHub does.
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/refs/heads/main", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity, github.ErrorResponse{
			Message: "Update is not a fast forward",
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)

	_, err := client.CommitFiles(context.Background(), owner, repo, basePath, files, "test commit")
	if err == nil {
		t.Fatal("CommitFiles returned nil error, want ErrOutputModifiedExternally")
	}
	if !errors.Is(err, ErrOutputModifiedExternally) {
		t.Errorf("CommitFiles error = %v, want it to wrap ErrOutputModifiedExternally", err)
	}
}

// --- rate limit then retry succeeds ------------------------------------------

func TestCommitFiles_RateLimitThenRetrySucceeds(t *testing.T) {
	const owner, repo = "acme", "output-repo"
	basePath := "acme/proj/prod"
	files := map[string][]byte{"main.tf": []byte("content")}

	var repoCalls int32

	mux := http.NewServeMux()

	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&repoCalls, 1)
		if n == 1 {
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(200*time.Millisecond).Unix()))
			writeJSON(t, w, http.StatusForbidden, github.ErrorResponse{Message: "API rate limit exceeded"})
			return
		}
		writeJSON(t, w, http.StatusOK, github.Repository{DefaultBranch: github.String("main")})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/ref/heads/main", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.Reference{
			Ref:    github.String("refs/heads/main"),
			Object: &github.GitObject{Type: github.String("commit"), SHA: github.String("base123")},
		})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/commits/base123", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.Commit{SHA: github.String("base123"), Tree: &github.Tree{SHA: github.String("basetree1")}})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/blobs", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, github.Blob{SHA: github.String("blobsha-1")})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/trees", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, github.Tree{SHA: github.String("newtree1")})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/commits", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusCreated, github.Commit{SHA: github.String("commit456")})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/git/refs/heads/main", owner, repo), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.Reference{
			Ref:    github.String("refs/heads/main"),
			Object: &github.GitObject{Type: github.String("commit"), SHA: github.String("commit456")},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)

	sha, err := client.CommitFiles(context.Background(), owner, repo, basePath, files, "test commit")
	if err != nil {
		t.Fatalf("CommitFiles returned error: %v", err)
	}
	if sha != "commit456" {
		t.Errorf("CommitFiles sha = %q, want %q", sha, "commit456")
	}
	if got := atomic.LoadInt32(&repoCalls); got != 2 {
		t.Errorf("repo endpoint called %d times, want 2 (one rate-limited, one retry)", got)
	}
}

// --- GetFile ------------------------------------------------------------------

func TestGetFile_Success(t *testing.T) {
	const owner, repo, path = "acme", "output-repo", "acme/proj/prod/main.tf"
	content := "resource \"null_resource\" \"x\" {}\n"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, github.RepositoryContent{
			Type:     github.String("file"),
			Encoding: github.String("base64"),
			Name:     github.String("main.tf"),
			Path:     github.String(path),
			Content:  github.String(base64.StdEncoding.EncodeToString([]byte(content))),
			SHA:      github.String("filesha1"),
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)

	got, sha, err := client.GetFile(context.Background(), owner, repo, path)
	if err != nil {
		t.Fatalf("GetFile returned error: %v", err)
	}
	if string(got) != content {
		t.Errorf("GetFile content = %q, want %q", got, content)
	}
	if sha != "filesha1" {
		t.Errorf("GetFile sha = %q, want %q", sha, "filesha1")
	}
}

func TestGetFile_NotFound(t *testing.T) {
	const owner, repo, path = "acme", "output-repo", "acme/proj/prod/missing.tf"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path), func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, github.ErrorResponse{Message: "Not Found"})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)

	_, _, err := client.GetFile(context.Background(), owner, repo, path)
	if err == nil {
		t.Fatal("GetFile returned nil error, want ErrFileNotFound")
	}
	if !errors.Is(err, ErrFileNotFound) {
		t.Errorf("GetFile error = %v, want it to wrap ErrFileNotFound", err)
	}
}
