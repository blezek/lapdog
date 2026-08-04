import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],

  // The bundle is embedded into the Go binary, so it is built straight into the
  // package that embeds it. There is no separate copy step to forget.
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    // Charts and tables are the bulk of the bundle and there is no network to
    // optimise for — the assets are served from the same executable over
    // loopback. A single chunk keeps the output simple.
    chunkSizeWarningLimit: 1500,
  },

  server: {
    port: 5173,
    // During development the interface runs from Vite and talks to the Go API,
    // so both look like one origin to the browser and no CORS handling is needed
    // in the server. Start the backend with:
    //   ./dist/lapdogctl serve .dataset.db
    proxy: {
      '/api': { target: 'http://127.0.0.1:47047', changeOrigin: true },
      '/icons': { target: 'http://127.0.0.1:47047', changeOrigin: true },
    },
  },
})
