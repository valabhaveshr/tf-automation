package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"tf-automation/backend/internal/catalog"
	"tf-automation/backend/internal/codegen"
	"tf-automation/backend/internal/data"
	"tf-automation/backend/internal/output"
)

// resolvedModule bundles everything one module_selections entry needs
// across preview/create/update/retry: the resolved schema row, its parsed
// metadata.yaml, its parsed VarSchema[], and the (mutable -- naming merges
// into it) instance input.
type resolvedModule struct {
	schema *data.ModuleSchema
	meta   catalog.ModuleMetadata
	vars   []codegen.VarSchema
	input  moduleInstanceInput
}

// resolveModules resolves every module_selections entry's pinned-or-latest
// schema (per ModuleInstanceInput.pinned_version's documented semantics:
// omit to pin latest at submission time, set to keep an existing pin) and
// decodes its cached metadata + VarSchema[]. Does not touch naming.
func (s *Server) resolveModules(ctx context.Context, selections []moduleInstanceInput) ([]*resolvedModule, error) {
	if len(selections) == 0 {
		return nil, asValidation(fmt.Errorf("codegen: record has zero module selections"))
	}

	out := make([]*resolvedModule, 0, len(selections))
	for _, sel := range selections {
		var ref string
		if sel.PinnedVersion != nil {
			ref = *sel.PinnedVersion
		}
		ms, err := s.resolveModuleSchema(ctx, sel.ModuleName, ref)
		if err != nil {
			if errors.Is(err, data.ErrNotFound) {
				return nil, notFound(fmt.Sprintf("module %q (version %q) is not catalog-eligible or does not exist", sel.ModuleName, ref))
			}
			return nil, err
		}

		var meta catalog.ModuleMetadata
		if err := json.Unmarshal(ms.Metadata, &meta); err != nil {
			return nil, fmt.Errorf("api: module %q: decoding cached metadata: %w", sel.ModuleName, err)
		}
		var vars []codegen.VarSchema
		if err := json.Unmarshal(ms.Variables, &vars); err != nil {
			return nil, fmt.Errorf("api: module %q: decoding cached variables: %w", sel.ModuleName, err)
		}

		selCopy := sel
		if selCopy.Instances == nil {
			selCopy.Instances = map[string]map[string]any{}
		}

		out = append(out, &resolvedModule{schema: ms, meta: meta, vars: vars, input: selCopy})
	}
	return out, nil
}

// renderFiles builds codegen.RecordInput from resolved modules and calls
// codegen.RenderRootFiles. codegen's own validation (required fields,
// instance-name format, etc.) IS the authoritative validation per
// plan/ARCHITECTURE.md -- any error it returns is tagged as a
// validationError (mapped to 400 with the underlying message by writeErr),
// never re-implemented here.
func (s *Server) renderFiles(resolved []*resolvedModule) (map[string][]byte, error) {
	modules := make([]codegen.ModuleInput, 0, len(resolved))
	for _, rm := range resolved {
		modules = append(modules, codegen.ModuleInput{
			ModuleName:    rm.input.ModuleName,
			PinnedVersion: rm.schema.VersionTag,
			Variables:     rm.vars,
			Instances:     rm.input.Instances,
		})
	}

	files, err := codegen.RenderRootFiles(codegen.RecordInput{
		Modules:        modules,
		ModulesRepoOrg: s.ModulesRepoOwner,
	})
	if err != nil {
		return nil, asValidation(err)
	}
	return files, nil
}

// maybeRenderBackendFile implements the "generate backend.tf once, never
// again" rule: it checks (via output.Client.GetFile) whether backend.tf
// already exists at outputPath, and only if it does NOT, renders it via
// codegen.RenderBackendFile. Called identically from preview (so the
// preview accurately reflects what a save would commit, per the "preview
// and save share one code path" byte-parity locked decision), create,
// update, and retry-sync.
func (s *Server) maybeRenderBackendFile(ctx context.Context, outputPath string) (name string, content []byte, include bool) {
	_, _, err := s.Output.GetFile(ctx, s.OutputRepoOwner, s.OutputRepoName, outputPath+"/backend.tf")
	if err == nil {
		return "", nil, false
	}
	if !errors.Is(err, output.ErrFileNotFound) {
		// Can't determine (transient GitHub error, rate limit exhausted,
		// etc.) -- conservatively skip generating it rather than risk
		// clobbering an existing backend.tf this check simply failed to
		// see. The record's own sync_status=failed path (if the ensuing
		// commit also fails) surfaces the underlying problem for retry.
		log.Printf("api: could not check for existing backend.tf at %q, skipping generation this save: %v", outputPath, err)
		return "", nil, false
	}
	content = codegen.RenderBackendFile(s.StateBackendType, s.StateBackendBucket, outputPath)
	return "backend.tf", content, true
}

func filesToStrings(files map[string][]byte) map[string]string {
	out := make(map[string]string, len(files))
	for k, v := range files {
		out[k] = string(v)
	}
	return out
}
