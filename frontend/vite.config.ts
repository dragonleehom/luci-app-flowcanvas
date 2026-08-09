import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: '../luci-app-flowcanvas/htdocs/luci-static/resources/flowcanvas',
    emptyOutDir: true,
    sourcemap: false,
    manifest: true,
    target: 'es2022',
  },
  server: {
    port: 5173,
    strictPort: true,
  },
})
