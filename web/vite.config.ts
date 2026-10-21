import { defineConfig } from 'vitest/config';

export default defineConfig({
  // relative paths so the build works from any folder a native app bundles it into
  base: './',
  server: {
    host: true, // reachable from a phone or emulator on the same network
    proxy: { '/v1': 'http://localhost:8080' },
  },
  build: {
    target: 'es2019', // older Android System WebView and iOS 13 WKWebView
    chunkSizeWarningLimit: 1600,
    rollupOptions: {
      input: { main: 'index.html', harness: 'harness.html' },
      output: { manualChunks: { phaser: ['phaser'] } },
    },
  },
  test: { environment: 'jsdom' },
});
