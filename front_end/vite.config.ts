import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // В деве фронт живёт на 5173, бэк на 8080. Прокси делает то же,
    // что nginx в проде: браузер видит один origin, CORS не нужен.
    proxy: { '/api': 'http://localhost:8080' },
  },
})
