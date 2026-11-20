import type { APIRoute } from 'astro';
import { pollerUrl } from '@/lib/services';

export const GET: APIRoute = async () => {
  const fqdn = pollerUrl();

  try {
    const res = await fetch(`${fqdn}/rss`, {
      headers: { 'Accept': 'application/json' },
    });
    if (!res.ok) {
      return new Response(JSON.stringify({ error: `Upstream error: ${res.status}` }), {
        status: res.status,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    const data = await res.json();
    return new Response(JSON.stringify(data), {
      headers: { 'Content-Type': 'application/json' },
    });
  } catch (e: any) {
    return new Response(JSON.stringify({ error: 'Failed to fetch feeds' }), {
      status: 500,
      headers: { 'Content-Type': 'application/json' },
    });
  }
};
