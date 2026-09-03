import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
  },
  envPrefix: ['VITE_', 'TAURI_'],
  build: {
    target: 'chrome105',
    minify: !process.env.TAURI_DEBUG ? 'esbuild' : false,
    sourcemap: Boolean(process.env.TAURI_DEBUG),
  },
})
