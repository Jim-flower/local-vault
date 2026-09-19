import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

function normalizeBasePath(value) {
  const path = String(value || '/').trim()
  if (!path || path === '/') return '/'
  return `/${path.replace(/^\/+|\/+$/g, '')}/`
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  return {
    base: normalizeBasePath(env.VITE_BASE_PATH),
    plugins: [react()],
    build: { outDir: 'dist' },
  }
})
