package api

import (
	"context"
	"errors"
	"sort"
	"strings"

	"tf-automation/backend/internal/output"
)

// buildDiff fetches each generated file's currently-committed content (via
// output.Client.GetFile) at outputPath and produces a unified diff against
// newFiles, for the edit-preview PUT /records/{id}/preview response's
// `diff` field. A file that doesn't exist yet in the output repo is
// treated as empty (a pure-addition diff); a file whose content is
// unchanged contributes nothing to the result.
func (s *Server) buildDiff(ctx context.Context, outputPath string, newFiles map[string][]byte) (string, error) {
	names := make([]string, 0, len(newFiles))
	for name := range newFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		oldContent, _, err := s.Output.GetFile(ctx, s.OutputRepoOwner, s.OutputRepoName, outputPath+"/"+name)
		if err != nil {
			if !errors.Is(err, output.ErrFileNotFound) {
				return "", err
			}
			oldContent = nil
		}
		if d := unifiedDiff(name, string(oldContent), string(newFiles[name])); d != "" {
			b.WriteString(d)
		}
	}
	return b.String(), nil
}
