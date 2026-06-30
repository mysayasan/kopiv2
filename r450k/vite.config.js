import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Static build for Cloudflare Pages. Output goes to dist/ which Pages serves as-is.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    sourcemap: false,
  },
});
