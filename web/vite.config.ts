import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'

// The app's stylesheet must not hold up the first paint: the page's own loading screen (index.html) has its styles
// inline and is drawn at once, the stylesheet arrives meanwhile. The loading screen leaves only after it has arrived.
const lateCss = (): Plugin => ({
  name: 'vd-late-css',
  apply: 'build',
  transformIndexHtml: { order: 'post', handler: html => html.replace(/<link rel="stylesheet"([^>]*?)>/g, '<link rel="stylesheet" media="print" onload="this.media=\'all\'"$1>') },
})

export default defineConfig({
  base: '/app/',
  plugins: [react(), lateCss()],
  server: { proxy: { '/api': 'http://127.0.0.1:8099' } },
  build: { target: 'es2020', assetsInlineLimit: 2048 },
})
