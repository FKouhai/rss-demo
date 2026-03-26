import type { APIRoute } from 'astro';

export const GET: APIRoute = () => {
  return new Response(JSON.stringify({ status: 'healthy' }), {
    headers: { 'Content-Type': 'application/json' },
  });
};
