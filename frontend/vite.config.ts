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
  test: {
    environment: 'jsdom',
    globals: true,
  },
});
