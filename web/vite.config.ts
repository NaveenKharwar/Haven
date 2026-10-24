import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: { '/api': 'http://localhost:8484' },
  },
  // assetsInlineLimit 0: never inline fonts as data: URLs, which Haven's
  // security policy (CSP) blocks. Every asset is served as a normal file.
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 800, assetsInlineLimit: 0 },
})
