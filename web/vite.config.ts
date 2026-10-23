import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// 开发期 /api 代理到本地 Go 服务（docs/04 §1）；构建产物由 Makefile 拷贝到 backend/internal/web/dist。
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8530',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    // 每份 spec 跑之前执行：注册「用例结束即卸载」的 afterEach（见 src/test/setup.ts）。
    setupFiles: ['./src/test/setup.ts'],
  },
})
