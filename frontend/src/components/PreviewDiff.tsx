import { useMemo, useState } from 'react';
import type { PreviewResult } from '../api/types';

/**
 * Renders a PreviewResult: a tabbed view over the generated file map
 * (path -> content), plus a unified-diff view when `diff` is present (only
 * populated for PUT /api/records/:id/preview, edit-preview against the
 * currently committed version). A plain-text diff render is intentionally
 * simple here — no diff library — per WS-H scope.
 */
export interface PreviewDiffProps {
  result: PreviewResult;
  hasSensitiveVariables?: boolean;
}

type ViewMode = 'files' | 'diff';

export default function PreviewDiff({ result, hasSensitiveVariables }: PreviewDiffProps) {
  const filePaths = useMemo(() => Object.keys(result.files).sort(), [result.files]);
  const [mode, setMode] = useState<ViewMode>('files');
  const [activeFile, setActiveFile] = useState<string | undefined>(filePaths[0]);

  const currentFile = activeFile && result.files[activeFile] !== undefined ? activeFile : filePaths[0];

  const suggestedNameEntries = Object.entries(result.suggested_names ?? {});

  return (
    <div className="preview-diff">
      {hasSensitiveVariables && (
        <div className="warning-banner">
          This record includes sensitive variable values. Per Phase 1's locked decision, they are
          still written into the generated *.auto.tfvars files in plaintext — secrets-manager
          handling is deferred to Phase 2.
        </div>
      )}

      {suggestedNameEntries.length > 0 && (
        <div className="field-help" style={{ marginBottom: 12 }}>
          <strong>Suggested names (tentative, not yet allocated):</strong>
          <ul>
            {suggestedNameEntries.map(([key, name]) => (
              <li key={key}>
                {key}: {name}
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="tab-bar">
        <button type="button" className={mode === 'files' ? 'active' : ''} onClick={() => setMode('files')}>
          Files
        </button>
        {result.diff && (
          <button type="button" className={mode === 'diff' ? 'active' : ''} onClick={() => setMode('diff')}>
            Diff
          </button>
        )}
      </div>

      {mode === 'files' && (
        <div>
          <div className="tab-bar">
            {filePaths.map((path) => (
              <button
                key={path}
                type="button"
                className={path === currentFile ? 'active' : ''}
                onClick={() => setActiveFile(path)}
              >
                {path}
              </button>
            ))}
          </div>
          {filePaths.length === 0 && <p>No files to preview yet.</p>}
          {currentFile && <pre className="code-preview">{result.files[currentFile]}</pre>}
        </div>
      )}

      {mode === 'diff' && result.diff && <UnifiedDiffView diff={result.diff} />}
    </div>
  );
}

function UnifiedDiffView({ diff }: { diff: string }) {
  const lines = diff.split('\n');
  return (
    <pre className="code-preview">
      {lines.map((line, i) => {
        let className = '';
        if (line.startsWith('+') && !line.startsWith('+++')) className = 'diff-add';
        else if (line.startsWith('-') && !line.startsWith('---')) className = 'diff-del';
        return (
          <div key={i} className={className}>
            {line.length === 0 ? ' ' : line}
          </div>
        );
      })}
    </pre>
  );
}
