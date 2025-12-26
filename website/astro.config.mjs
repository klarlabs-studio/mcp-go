import { defineConfig } from 'astro/config';

export default defineConfig({
  site: 'https://felixgeelhaar.github.io',
  base: '/mcp-go',
  build: {
    assets: '_assets'
  }
});
