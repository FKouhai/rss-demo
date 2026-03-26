const LOCATOR_URL = process.env.LOCATOR_URL;

export function locatorEndpoint(): string | null {
  return LOCATOR_URL ?? null;
}

async function tryLookup(name: string): Promise<string | null> {
  if (!LOCATOR_URL) return null;
  try {
    const res = await fetch(`${LOCATOR_URL}/services`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ service: name }),
      signal: AbortSignal.timeout(5000),
    });
    if (!res.ok) return null;
    const data = await res.json();
    const fqdn: string | undefined = data.fqdn;
    if (!fqdn) return null;
    // Mirror the Go bootstrap library: add http:// if no scheme is present.
    return fqdn.startsWith('http://') || fqdn.startsWith('https://')
      ? fqdn
      : `http://${fqdn}`;
  } catch {
    return null;
  }
}

/**
 * Look up a service's FQDN from the service registry, retrying on failure.
 * Handles cold-start races where a service hasn't registered yet.
 */
export async function lookupService(
  name: string,
  { retries = 3, delayMs = 1000 }: { retries?: number; delayMs?: number } = {},
): Promise<string | null> {
  for (let attempt = 0; attempt <= retries; attempt++) {
    if (attempt > 0) {
      await new Promise(resolve => setTimeout(resolve, delayMs));
    }
    const fqdn = await tryLookup(name);
    if (fqdn) return fqdn;
  }
  return null;
}
