import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// defineConfig 统一开发代理与构建输出，避免前后端联调地址和嵌入目录分叉。
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/tokenapi': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
    },
  },
})
