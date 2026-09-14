import { defineConfig } from 'astro/config';

export default defineConfig({
  server: { host: '0.0.0.0', port: 5344 },
  vite: {
    server: {
      // Allow the Hoplite Preview tunnel hostname (and any other host).
      allowedHosts: true,
    },
  },
});
