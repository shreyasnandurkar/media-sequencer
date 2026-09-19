import react from '@vitejs/plugin-react';
// vitest/config re-exports Vite's defineConfig with the `test` block typed.
import { defineConfig } from 'vitest/config';

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    // The shared scheduler vectors live in ../testdata, outside this package.
    // Vite refuses to serve files above the project root unless we say so.
    fs: { allow: ['..'] },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
  },
});
