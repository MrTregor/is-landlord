/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { PrimeVueResolver } from '@primevue/auto-import-resolver'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    // Компоненты PrimeVue подключаются автоматически (13 Фронтенд, раздел 1).
    Components({
      dirs: [],
      resolvers: [PrimeVueResolver()],
      dts: 'src/components.d.ts',
    }),
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: { '/api': 'http://localhost:8080' },
    // В WSL изменения файлов на диске Windows (/mnt/...) не приходят событиями — опрашивать файлы.
    watch: process.env.WSL_DISTRO_NAME ? { usePolling: true, interval: 300 } : undefined,
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.spec.ts'],
  },
})
