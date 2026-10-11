import { Fragment, type ReactNode } from 'react';
import { ActionIcon, Box, CopyButton, ScrollArea, Tooltip } from '@mantine/core';
import { IconCheck, IconCopy } from '@tabler/icons-react';
import classes from './HclCode.module.css';

/**
 * Read-only code block with lightweight HCL highlighting (block keywords,
 * strings, numbers, booleans, comments, attribute names) and a copy button.
 * Deliberately tiny instead of a full highlighter: generated .tf/.tfvars
 * only use a small slice of HCL.
 */
export default function HclCode({ code, diff = false, maxHeight = 520 }: { code: string; diff?: boolean; maxHeight?: number }) {
  return (
    <Box className={classes.wrapper}>
      <CopyButton value={code} timeout={1500}>
        {({ copied, copy }) => (
          <Tooltip label={copied ? 'Copied' : 'Copy'} withArrow position="left">
            <ActionIcon className={classes.copy} variant="default" onClick={copy} aria-label="Copy code">
              {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
            </ActionIcon>
          </Tooltip>
        )}
      </CopyButton>
      <ScrollArea.Autosize mah={maxHeight} type="auto">
        <pre className={classes.pre}>{diff ? renderDiff(code) : highlightHcl(code)}</pre>
      </ScrollArea.Autosize>
    </Box>
  );
}

const BLOCK_KEYWORDS = new Set(['module', 'variable', 'resource', 'data', 'terraform', 'backend', 'provider', 'output', 'locals']);
const LITERALS = new Set(['true', 'false', 'null']);

function highlightHcl(code: string): ReactNode[] {
  return code.split('\n').map((line, i) => (
    <Fragment key={i}>
      {highlightLine(line)}
      {'\n'}
    </Fragment>
  ));
}

const TOKEN = /("(?:[^"\\]|\\.)*")|(#.*$|\/\/.*$)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_][A-Za-z0-9_-]*)(\s*=(?!=))?/g;

function highlightLine(line: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  TOKEN.lastIndex = 0;
  const leading = line.match(/^\s*([A-Za-z_][A-Za-z0-9_-]*)/)?.[1];
  while ((m = TOKEN.exec(line))) {
    if (m.index > last) out.push(line.slice(last, m.index));
    const [text, str, comment, num, ident, assign] = m;
    if (str) out.push(span(classes.string, str, m.index));
    else if (comment) out.push(span(classes.comment, comment, m.index));
    else if (num) out.push(span(classes.number, num, m.index));
    else if (ident) {
      if (assign) {
        out.push(span(classes.attr, ident, m.index), assign);
      } else if (LITERALS.has(ident)) {
        out.push(span(classes.literal, ident, m.index));
      } else if (ident === leading && BLOCK_KEYWORDS.has(ident)) {
        out.push(span(classes.keyword, ident, m.index));
      } else {
        out.push(ident);
      }
    }
    last = m.index + text.length;
  }
  if (last < line.length) out.push(line.slice(last));
  return out;
}

function span(className: string, text: string, key: number) {
  return (
    <span key={key} className={className}>
      {text}
    </span>
  );
}

function renderDiff(code: string): ReactNode[] {
  return code.split('\n').map((line, i) => {
    let cls: string | undefined;
    if (line.startsWith('+++') || line.startsWith('---')) cls = classes.diffFile;
    else if (line.startsWith('@@')) cls = classes.diffHunk;
    else if (line.startsWith('+')) cls = classes.diffAdd;
    else if (line.startsWith('-')) cls = classes.diffDel;
    return (
      <span key={i} className={cls}>
        {line}
        {'\n'}
      </span>
    );
  });
}
