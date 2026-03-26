import { useState, useEffect, useRef } from 'preact/hooks';

const STORAGE_KEY = 'rss-feed-config';

type Status = { ok: true } | { ok: false; error: string } | null;

const ConfigPage = () => {
  const [feeds, setFeeds] = useState<string[]>([]);
  const [inputValue, setInputValue] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [status, setStatus] = useState<Status>(null);
  const statusTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) setFeeds(JSON.parse(stored));
    } catch {}
  }, []);

  const showStatus = (s: Status) => {
    if (statusTimer.current) clearTimeout(statusTimer.current);
    setStatus(s);
    statusTimer.current = setTimeout(() => setStatus(null), 5000);
  };

  const addFeed = () => {
    const url = inputValue.trim();
    if (!url) return;
    if (feeds.includes(url)) {
      showStatus({ ok: false, error: 'That URL is already in the list.' });
      return;
    }
    try {
      new URL(url);
    } catch {
      showStatus({ ok: false, error: 'Please enter a valid URL.' });
      return;
    }
    setFeeds(prev => [...prev, url]);
    setInputValue('');
  };

  const removeFeed = (url: string) => {
    setFeeds(prev => prev.filter(f => f !== url));
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter') addFeed();
  };

  const handleSubmit = async () => {
    if (feeds.length === 0) {
      showStatus({ ok: false, error: 'Add at least one feed URL before saving.' });
      return;
    }
    setSubmitting(true);
    try {
      const res = await fetch('/api/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ rss_feeds: feeds }),
      });
      const data = await res.json();
      if (!res.ok) {
        showStatus({ ok: false, error: data.error ?? `Error ${res.status}` });
      } else {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(feeds));
        showStatus({ ok: true });
      }
    } catch {
      showStatus({ ok: false, error: 'Request failed. Check that the service locator is reachable.' });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="p-4 container mx-auto max-w-2xl">
      <h1 className="text-3xl md:text-4xl font-bold mb-2">Configure Feeds</h1>
      <p className="text-sm text-muted-foreground mb-6">
        RSS feed URLs to poll. Changes take effect immediately — the poller restarts its cycle with the new list.
      </p>

      <div className="flex gap-2 mb-4">
        <input
          type="url"
          placeholder="https://example.com/feed.xml"
          value={inputValue}
          onInput={(e) => setInputValue((e.target as HTMLInputElement).value)}
          onKeyDown={handleKeyDown}
          className="flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        />
        <button
          onClick={addFeed}
          className="rounded-md bg-primary text-primary-foreground px-4 py-2 text-sm font-medium shadow hover:bg-primary/90 transition-colors"
        >
          Add
        </button>
      </div>

      {feeds.length === 0 ? (
        <p className="text-sm text-muted-foreground py-8 text-center border border-dashed rounded-md">
          No feeds configured. Add a URL above.
        </p>
      ) : (
        <ul className="space-y-2 mb-6">
          {feeds.map(url => (
            <li key={url} className="flex items-center justify-between rounded-md border px-3 py-2 text-sm">
              <span className="truncate mr-4 text-foreground">{url}</span>
              <button
                onClick={() => removeFeed(url)}
                className="shrink-0 text-muted-foreground hover:text-destructive transition-colors"
                aria-label={`Remove ${url}`}
              >
                Remove
              </button>
            </li>
          ))}
        </ul>
      )}

      {status && (
        <div className={`mb-4 rounded-md border px-4 py-3 text-sm ${
          status.ok
            ? 'border-green-200 bg-green-50 text-green-800 dark:border-green-800 dark:bg-green-950 dark:text-green-200'
            : 'border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-200'
        }`}>
          {status.ok ? 'Configuration saved. The poller is now running with the updated feed list.' : status.error}
        </div>
      )}

      <button
        onClick={handleSubmit}
        disabled={submitting || feeds.length === 0}
        className="w-full rounded-md bg-primary text-primary-foreground px-4 py-2 text-sm font-medium shadow hover:bg-primary/90 transition-colors disabled:pointer-events-none disabled:opacity-50"
      >
        {submitting ? 'Saving...' : 'Save & Apply'}
      </button>
    </div>
  );
};

export default ConfigPage;
