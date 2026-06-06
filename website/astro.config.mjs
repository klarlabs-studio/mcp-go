import { defineConfig } from 'astro/config';

export default defineConfig({
  site: 'https://klarlabs-studio.github.io',
  base: '/mcp-go',
  build: {
    assets: '_assets'
  }
});
