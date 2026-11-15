import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import wails from '@wailsio/runtime/plugins/vite'

// https://vitejs.dev/config/
export default defineConfig({
  // The Wails dev runner starts the Vite dev server on this port; strictPort
  // keeps the embedded app URL in sync.
  server: {
    host: '127.0.0.1',
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [svelte(), wails('./bindings')],
  build: {
    rollupOptions: {
      // index.html is the main window; settings.html is the Settings window;
      // import.html is the Import window.
      input: {
        main: 'index.html',
        settings: 'settings.html',
        import: 'import.html',
      },
    },
  },
})
