import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({ plugins: [react()], server: { port: 5190, proxy: { '/api': { target: 'https://154.8.225.152:9186', changeOrigin: true, secure: false, rewrite: path => path.replace(/^\/api/, '/v1') } } } });
