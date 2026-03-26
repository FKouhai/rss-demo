import type { APIRoute } from 'astro';
import { lookupService, locatorEndpoint } from '@/lib/locator';

export const GET: APIRoute = async () => {
  const fqdn = await lookupService('poller');
  if (!fqdn) {
    return new Response(JSON.stringify({ error: 'poller not registered in service locator' }), {
      status: 503,
      headers: { 'Content-Type': 'application/json' },
    });
  }
  try {
    const res = await fetch(`${fqdn}/config/feeds`, {
      signal: AbortSignal.timeout(5000),
    });
    if (!res.ok) {
      return new Response(JSON.stringify({ rss_feeds: [] }), {
        headers: { 'Content-Type': 'application/json' },
      });
    }
    const data = await res.json();
    return new Response(JSON.stringify(data), {
      headers: { 'Content-Type': 'application/json' },
    });
  } catch {
    return new Response(JSON.stringify({ rss_feeds: [] }), {
      headers: { 'Content-Type': 'application/json' },
    });
  }
};

export const POST: APIRoute = async ({ request }) => {
  if (!locatorEndpoint()) {
    return new Response(JSON.stringify({ error: 'LOCATOR_URL not configured' }), {
      status: 500,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return new Response(JSON.stringify({ error: 'Invalid JSON body' }), {
      status: 400,
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
    const res = await fetch(`${fqdn}/config`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      return new Response(JSON.stringify({ error: `Poller rejected config: ${res.status}` }), {
        status: res.status,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    return new Response(JSON.stringify({ ok: true }), {
      headers: { 'Content-Type': 'application/json' },
    });
  } catch {
    return new Response(JSON.stringify({ error: 'Failed to reach rss-poller' }), {
      status: 500,
      headers: { 'Content-Type': 'application/json' },
    });
  }
};
