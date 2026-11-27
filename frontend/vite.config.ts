import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwind from '@tailwindcss/vite'
import { readFileSync } from 'node:fs'

export default defineConfig({
  base: './',
  plugins: [react(), tailwind(), {
    name: 'font-licenses',
    generateBundle() {
      for (const family of ['inter', 'jetbrains-mono']) {
        this.emitFile({ type: 'asset', fileName: 'assets/' + family + '-OFL.txt',
          source: readFileSync(new URL('./node_modules/@fontsource-variable/' + family + '/LICENSE', import.meta.url), 'utf8') })
      }
    }
  }],
  build: { outDir: '../pkg/dashboard/web/dist', emptyOutDir: true, assetsInlineLimit: 0 },
  test: { environment: 'jsdom', setupFiles: ['./test/setup.ts'], restoreMocks: true },
})

