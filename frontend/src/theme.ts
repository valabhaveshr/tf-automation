import { createTheme, type MantineColorsTuple } from '@mantine/core';

// Brand: a deep ocean blue, used as the primary color throughout.
const brand: MantineColorsTuple = [
  '#e7f3ff',
  '#d0e3ff',
  '#a0c4fb',
  '#6ca3f7',
  '#4287f3',
  '#2a76f2',
  '#1b6df3',
  '#0c5cd8',
  '#0051c2',
  '#0046ac',
];

export const theme = createTheme({
  primaryColor: 'brand',
  colors: { brand },
  defaultRadius: 'md',
  fontFamily:
    'Inter, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans", sans-serif',
  fontFamilyMonospace: '"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
  headings: { fontWeight: '650' },
  cursorType: 'pointer',
  components: {
    Card: { defaultProps: { withBorder: true, radius: 'lg' } },
    Paper: { defaultProps: { radius: 'lg' } },
    Badge: { defaultProps: { radius: 'sm' } },
  },
});

/** Accent colors cycled across module panes in the editor, so each module's pane is easy to tell apart. */
export const MODULE_ACCENTS = ['brand', 'teal', 'grape', 'orange', 'cyan', 'pink', 'lime', 'indigo'] as const;
