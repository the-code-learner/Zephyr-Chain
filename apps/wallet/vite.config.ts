import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const nodeTarget = process.env.ZEPHYR_WALLET_DEV_NODE ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/health': nodeTarget,
      '/v1': nodeTarget,
      '/metrics': nodeTarget
    }
  }
})
