// @ts-check
import { defineConfig } from 'astro/config';
import { loadEnv } from "vite";
import preact from '@astrojs/preact';
import node from '@astrojs/node';

import tailwindcss from '@tailwindcss/vite';

const { NODE_TLS_REJECT_UNAUTHORIZED, POLLER_ENDPOINT } = loadEnv(process.env.NODE_ENV || 'development', process.cwd(), "");
process.env.NODE_TLS_REJECT_UNAUTHORIZED = NODE_TLS_REJECT_UNAUTHORIZED;
process.env.POLLER_ENDPOINT = process.env.POLLER_ENDPOINT || POLLER_ENDPOINT;

// https://astro.build/config
export default defineConfig({
  output: 'server',
  server: {
    host: '0.0.0.0'
  },
  integrations: [preact()],

  adapter: node({
    mode: 'standalone',
  }),

  vite: {
    plugins: [tailwindcss()],
  }
});
