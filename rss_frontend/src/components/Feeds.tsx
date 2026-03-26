import { useState, useEffect, useRef } from 'preact/hooks';
import DOMPurify from 'dompurify';
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const STORAGE_KEY = 'rss-read-items';
const REFRESH_MS = 2 * 60 * 1000;
const RECOVERY_MS = 10 * 1000;

// Cycle through a palette so each article always gets the same accent colour.
const ACCENT_COLORS = ['#3b82f6','#8b5cf6','#10b981','#f59e0b','#f43f5e','#06b6d4','#f97316','#ec4899'];

const stableAccent = (key: string): string => {
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = (hash << 5) - hash + key.charCodeAt(i);
    hash |= 0;
  }
  return ACCENT_COLORS[Math.abs(hash) % ACCENT_COLORS.length];
};

const sanitize = (html: string) =>
  typeof window === 'undefined' ? html : DOMPurify.sanitize(html);

const truncateHtml = (html: string, maxLength: number) => {
  const plainText = html.replace(/<[^>]*>?/gm, '');
  if (plainText.length <= maxLength) return html;
  return plainText.substring(0, maxLength) + '...';
};

const Feeds = ({ data: initialData, error: initialError }) => {
  const [data, setData] = useState<any[]>(Array.isArray(initialData) ? initialData : []);
  const [error, setError] = useState(initialError);
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState('');
  const [itemsPerPage, setItemsPerPage] = useState(16);
  const [expandedItems, setExpandedItems] = useState<Record<string, boolean>>({});
  const [readItems, setReadItems] = useState<Set<string>>(new Set());
  const [focusedIndex, setFocusedIndex] = useState<number | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const paginatedDataRef = useRef<any[]>([]);
  const focusedIndexRef = useRef<number | null>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => { focusedIndexRef.current = focusedIndex; }, [focusedIndex]);

  useEffect(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) setReadItems(new Set(JSON.parse(stored)));
    } catch {}
  }, []);

  useEffect(() => {
    const refresh = async () => {
      try {
        const res = await fetch('/api/feeds', { headers: { 'Accept': 'application/json' } });
        if (!res.ok) return;
        const fresh: any[] = await res.json();
        if (!Array.isArray(fresh) || fresh.length === 0) return;
        setError(undefined);
        setData(prev => {
          const seen = new Set(prev.map((item: any) => item.link || item.title));
          const newItems = fresh.filter((item: any) => !seen.has(item.link || item.title));
          if (prev.length === 0) return fresh;
          if (newItems.length === 0) return prev;
          if (toastTimer.current) clearTimeout(toastTimer.current);
          setToast(`${newItems.length} new item${newItems.length !== 1 ? 's' : ''}`);
          toastTimer.current = setTimeout(() => setToast(null), 4000);
          return [...newItems, ...prev];
        });
      } catch {}
    };

    const interval = error ? RECOVERY_MS : REFRESH_MS;
    const id = setInterval(refresh, interval);
    return () => clearInterval(id);
  }, [error]);

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
      if (e.key === 'j') {
        setFocusedIndex(i => Math.min((i ?? -1) + 1, paginatedDataRef.current.length - 1));
      } else if (e.key === 'k') {
        setFocusedIndex(i => Math.max((i ?? 1) - 1, 0));
      } else if (e.key === 'o') {
        const i = focusedIndexRef.current;
        if (i !== null) {
          const item = paginatedDataRef.current[i];
          if (item?.link) window.open(item.link, '_blank', 'noopener,noreferrer');
        }
      }
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, []);

  const markRead = (key: string) => {
    setReadItems(prev => {
      const next = new Set(prev);
      next.add(key);
      try { localStorage.setItem(STORAGE_KEY, JSON.stringify([...next])); } catch {}
      return next;
    });
  };

  const handleSearch = (value: string) => {
    setQuery(value);
    setPage(1);
    setFocusedIndex(null);
  };

  const handlePageChange = (newPage: number) => {
    setPage(newPage);
    setFocusedIndex(null);
  };

  const filteredData = query.trim()
    ? data.filter((item) => {
        const q = query.toLowerCase();
        const plainContent = (item.content || '').replace(/<[^>]*>?/gm, '').toLowerCase();
        return (item.title || '').toLowerCase().includes(q) || plainContent.includes(q);
      })
    : data;

  const totalPages = Math.max(1, Math.ceil(filteredData.length / itemsPerPage));
  const startIndex = (page - 1) * itemsPerPage;
  const paginatedData = filteredData.slice(startIndex, startIndex + itemsPerPage);
  paginatedDataRef.current = paginatedData;
  const hasMorePages = page < totalPages;

  const unreadCount = data.filter(item => !readItems.has(item.link || item.title)).length;

  if (error && data.length === 0) return (
    <div className="p-8 text-center space-y-2">
      <p className="text-red-500">Error: {error}</p>
      <p className="text-sm text-muted-foreground">Retrying every 10 seconds...</p>
    </div>
  );

  if (data.length === 0) return (
    <div className="p-8 text-center space-y-2">
      <p className="text-muted-foreground">No feeds available yet.</p>
      <p className="text-sm text-muted-foreground">
        The poller may still be starting up, or no feeds have been configured.{' '}
        <a href="/config" className="underline hover:text-foreground transition-colors">Add RSS feeds</a> to get started.
      </p>
    </div>
  );

  return (
    <div className="w-full p-4 min-h-screen flex flex-col justify-between">
      {toast && (
        <div className="fixed top-4 right-4 z-50 rounded-lg border border-green-200 bg-green-50 px-4 py-3 text-sm font-medium text-green-800 shadow-lg dark:border-green-800 dark:bg-green-950 dark:text-green-200 animate-slide-in">
          {toast}
        </div>
      )}
      <div className="w-full max-w-screen-2xl mx-auto px-4">
        <div className="mb-8 text-center">
          <h1 className="text-4xl md:text-5xl font-bold mb-2 bg-gradient-to-r from-foreground via-foreground/80 to-foreground/50 bg-clip-text text-transparent">
            Latest Feeds
          </h1>
          {unreadCount > 0 && (
            <span className="inline-flex items-center gap-1.5 rounded-full bg-primary/10 px-3 py-1 text-xs font-medium text-primary">
              <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
              {unreadCount} unread
            </span>
          )}
        </div>

        <div className="mb-8 flex flex-wrap justify-center gap-3">
          <input
            type="search"
            placeholder="Search feeds..."
            value={query}
            onInput={(e) => handleSearch((e.target as HTMLInputElement).value)}
            className="w-full max-w-md rounded-lg border border-input bg-background/80 backdrop-blur-sm px-4 py-2.5 text-sm shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring transition-shadow"
          />
          <select
            value={itemsPerPage}
            onChange={(e) => { setItemsPerPage(Number((e.target as HTMLSelectElement).value)); setPage(1); }}
            className="rounded-lg border border-input bg-background/80 px-3 py-2.5 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {[16, 32, 64].map(n => <option key={n} value={n}>{n} per page</option>)}
          </select>
        </div>

        {filteredData.length === 0 && (
          <div className="p-8 text-center text-muted-foreground">No results for "{query}".</div>
        )}

        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-6">
          {paginatedData.map((item, idx) => {
            const stableKey = item.link || item.title;
            const isExpanded = expandedItems[stableKey] || false;
            const isRead = readItems.has(stableKey);
            const isFocused = focusedIndex === idx;
            const content = item.content || '';
            const isLongContent = content.replace(/<[^>]*>?/gm, '').length > 300;
            const displayContent = isLongContent && !isExpanded
              ? truncateHtml(content, 300)
              : content;
            const accent = stableAccent(stableKey);

            return (
              <Card
                key={stableKey}
                className={`
                  feed-card flex flex-col cursor-default border-t-4
                  transition-all duration-200
                  hover:-translate-y-1 hover:shadow-xl
                  ${isRead ? 'opacity-40' : ''}
                  ${isFocused ? 'ring-2 ring-ring shadow-lg' : ''}
                `}
                style={{
                  borderTopColor: accent,
                  animationDelay: `${idx * 30}ms`,
                }}
                onClick={() => setFocusedIndex(idx)}
              >
                <CardHeader className="pb-2">
                  <div className="flex items-start justify-between gap-2">
                    <CardTitle className="text-base leading-snug line-clamp-2">
                      {item.title}
                    </CardTitle>
                    {!isRead && (
                      <span
                        className="mt-1 h-2 w-2 shrink-0 rounded-full"
                        style={{ backgroundColor: accent }}
                        title="Unread"
                      />
                    )}
                  </div>
                  <CardDescription className="pt-1">
                    <a
                      href={item.link}
                      className="inline-flex items-center gap-1 text-xs font-medium transition-colors hover:underline"
                      style={{ color: accent }}
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={() => markRead(stableKey)}
                    >
                      View original post ↗
                    </a>
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex-grow pt-0">
                  <div
                    className="feeds-content text-sm text-gray-600 dark:text-gray-300 leading-relaxed"
                    dangerouslySetInnerHTML={{ __html: sanitize(displayContent) }}
                  />
                  {isLongContent && (
                    <button
                      onClick={() => setExpandedItems({
                        ...expandedItems,
                        [stableKey]: !isExpanded,
                      })}
                      className="mt-3 text-xs font-medium transition-colors hover:underline"
                      style={{ color: accent }}
                    >
                      {isExpanded ? '↑ Show less' : '↓ Read more'}
                    </button>
                  )}
                </CardContent>
              </Card>
            );
          })}
        </div>
      </div>

      {filteredData.length > 0 && (
        <div className="container mx-auto mt-10 flex justify-center items-center gap-4">
          <Button
            variant="outline"
            onClick={() => handlePageChange(Math.max(1, page - 1))}
            disabled={page === 1}
          >
            ← Previous
          </Button>
          <span className="text-sm text-muted-foreground tabular-nums">
            Page {page} of {totalPages}
          </span>
          <Button
            variant="outline"
            onClick={() => handlePageChange(Math.min(page + 1, totalPages))}
            disabled={!hasMorePages}
          >
            Next →
          </Button>
        </div>
      )}
    </div>
  );
};

export default Feeds;
