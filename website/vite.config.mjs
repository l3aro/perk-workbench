import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

const root = fileURLToPath(new URL('.', import.meta.url));

export default defineConfig({
  root: resolve(root, 'frontend'),
  // All emitted asset URLs are rooted at the site's /static/ mount.
  base: '/static/',
  plugins: [tailwindcss()],
  build: {
    outDir: resolve(root, 'internal/site/assets/dist'),
    emptyOutDir: true,
    // Emit .vite/manifest.json so the Go server can map logical entries
    // (site, demo) to their content-hashed filenames.
    manifest: true,
    rollupOptions: {
      input: {
        site: resolve(root, 'frontend/site.css'),
        demo: resolve(root, 'frontend/demo.js'),
        app: resolve(root, 'frontend/app.js'),
      },
    },
    target: 'es2018',
  },
});