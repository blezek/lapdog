import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],

  // The bundle is embedded into the Go binary, so it is built straight into the
  // package that embeds it. There is no separate copy step to forget.
  build: {
    outDir: '../internal/web/dist',
    // Not emptied, because that directory is not only build output.
    //
    // It holds a tracked .gitkeep, which is what gives //go:embed all:dist
    // something to match on a clean clone — without it every Go build fails at
    // compile time. Emptying the directory deleted that placeholder on every
    // build, so the repository showed it as deleted and a `make clean` left the
    // tree unable to compile at all. `make ui` removes the previous assets
    // explicitly instead, which keeps stale content-hashed files from piling up
    // without taking the placeholder with them.
    emptyOutDir: false,
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
