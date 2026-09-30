package catalog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"tf-automation/backend/internal/data"
)

// fakeSource is an in-memory ModulesRepoSource for tests -- no real GitHub
// calls. Files are keyed by ref then repo-relative path.
type fakeSource struct {
	tags  []string
	files map[string]map[string][]byte // ref -> path -> content
}

func newFakeSource() *fakeSource {
	return &fakeSource{files: make(map[string]map[string][]byte)}
}

func (f *fakeSource) addTag(tag string) {
	f.tags = append(f.tags, tag)
}

func (f *fakeSource) addFile(ref, path, content string) {
	if f.files[ref] == nil {
		f.files[ref] = make(map[string][]byte)
	}
	f.files[ref][path] = []byte(content)
}

func (f *fakeSource) ListTags(ctx context.Context) ([]string, error) {
	return f.tags, nil
}

func (f *fakeSource) GetFileAtRef(ctx context.Context, path, ref string) ([]byte, error) {
	byPath, ok := f.files[ref]
	if !ok {
		return nil, fmt.Errorf("fakeSource: no files recorded for ref %q", ref)
	}
	content, ok := byPath[path]
	if !ok {
		return nil, fmt.Errorf("fakeSource: %q not found at ref %q", path, ref)
	}
	return content, nil
}

// fakeSchemaStore is an in-memory ModuleSchemaStore for tests, standing in
// for WS-A's real Postgres-backed *data.Store. Keyed by (module_name,
// version_tag) like the real UNIQUE constraint, and mimics UpsertModuleSchema's
// "clear other is_latest rows for this module" behavior closely enough for
// this package's tests (Service never depends on more than that).
type fakeSchemaStore struct {
	mu   sync.Mutex
	rows map[string]*data.ModuleSchema // key: module_name + "@" + version_tag
}

func newFakeSchemaStore() *fakeSchemaStore {
	return &fakeSchemaStore{rows: make(map[string]*data.ModuleSchema)}
}

func (s *fakeSchemaStore) UpsertModuleSchema(ctx context.Context, ms *data.ModuleSchema) (*data.ModuleSchema, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ms.IsLatest {
		for _, row := range s.rows {
			if row.ModuleName == ms.ModuleName && row.VersionTag != ms.VersionTag {
				row.IsLatest = false
			}
		}
	}

	key := ms.ModuleName + "@" + ms.VersionTag
	out := *ms
	out.ID = uuid.New()
	out.ParsedAt = time.Now()
	s.rows[key] = &out

	copyOut := out
	return &copyOut, nil
}

// latest returns the row currently flagged is_latest for moduleName, or nil
// if there isn't one.
func (s *fakeSchemaStore) latest(moduleName string) *data.ModuleSchema {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.rows {
		if row.ModuleName == moduleName && row.IsLatest {
			return row
		}
	}
	return nil
}

func (s *fakeSchemaStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}
