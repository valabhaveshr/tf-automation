import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// WS-H frontend build config. Dev server proxies /api to the backend (WS-G),
// or to the OpenAPI mock server per plan/ARCHITECTURE.md Phase 0 exit criteria,
// via the TF_AUTOMATION_API_PROXY env var (defaults to localhost:8080).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.TF_AUTOMATION_API_PROXY ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        // Vendor code changes far less often than app code; separate chunks keep browser caches warm across deploys.
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
          mantine: ['@mantine/core', '@mantine/hooks', '@mantine/modals', '@mantine/notifications'],
          icons: ['@tabler/icons-react'],
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
  },
});
