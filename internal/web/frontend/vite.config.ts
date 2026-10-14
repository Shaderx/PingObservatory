import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  cacheDir: '.cache/vite',
  build: {
    outDir: 'dist',
    chunkSizeWarningLimit: 1100,
    rollupOptions: { output: { manualChunks: { map: ['maplibre-gl'], charts: ['echarts/core', 'echarts/charts', 'echarts/components', 'echarts/renderers'] } } },
  },
});
