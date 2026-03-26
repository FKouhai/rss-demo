import type { APIRoute } from 'astro';
import { lookupService, locatorEndpoint } from '@/lib/locator';

export const GET: APIRoute = async () => {
  if (!locatorEndpoint()) {
    return new Response(JSON.stringify({ error: 'LOCATOR_URL not configured' }), {
      status: 500,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  const fqdn = await lookupService('poller');
  if (!fqdn) {
    return new Response(JSON.stringify({ error: 'poller is not registered in the service locator' }), {
      status: 503,
      headers: { 'Content-Type': 'application/json' },
    });
  }

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
