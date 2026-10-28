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
    // 监听所有网卡，不只是本机。
    // 默认的 'localhost' 在 macOS 上会**只绑 IPv6 的 [::1]**：本机用
    // http://localhost:5173 能开，但 http://127.0.0.1:5173 直接连接被拒，
    // 局域网里的手机/平板/另一台机器更是连不上（Vite 自己也提示
    // "Network: use --host to expose"）。开发时经常要用手机看响应式布局，
    // 这里直接放开到 0.0.0.0，各终端都能访问。
    host: '0.0.0.0',
    port: 5173,
    // 端口被占用时直接报错退出，不要静默换到 5174（换了端口手机上打开的
    // 还是旧地址，会白等一轮超时）。
    strictPort: true,
    proxy: {
      '/api': {
        // 代理目标可用环境变量覆盖（默认本地 8530 的容器实例）：
        // RACCOON_API_TARGET=http://127.0.0.1:18531 npm run dev
        target: process.env.RACCOON_API_TARGET ?? 'http://127.0.0.1:8530',
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
