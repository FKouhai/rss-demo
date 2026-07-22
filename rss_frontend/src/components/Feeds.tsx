import { useState, useEffect, useCallback } from 'preact/hooks';
import { memo } from 'preact/compat';
import DOMPurify from 'dompurify';
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Drawer } from "./ui/drawer";
import { BookOpen, ExternalLink, Calendar, Compass, Search, ChevronDown } from "lucide-preact";

const STORAGE_KEY = 'rss-read-items';
const REFRESH_MS = 2 * 60 * 1000;
const RECOVERY_MS = 10 * 1000;

const ACCENT_COLORS = ['#3b82f6','#8b5cf6','#10b981','#f59e0b','#f43f5e','#06b6d4','#f97316','#ec4899'];

const stableAccent = (key: string): string => {
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = (hash << 5) - hash + key.charCodeAt(i);
    hash |= 0;
  }
  return ACCENT_COLORS[Math.abs(hash) % ACCENT_COLORS.length];
};

const TRAILING_LINK_PATTERNS = [
  'read full article', 'read more', 'read article', 'full article',
  'comments', 'leave a comment', 'continue reading',
];

const sanitize = (html: string): string => {
  if (typeof window === 'undefined') return html;
  const clean = DOMPurify.sanitize(html);
  const div = document.createElement('div');
  div.innerHTML = clean;
  div.querySelectorAll('a').forEach(a => {
    const text = (a.textContent || '').trim().toLowerCase();
    if (TRAILING_LINK_PATTERNS.some(p => text === p)) a.remove();
  });
  return div.innerHTML;
};

const stripTags = (html: string) => html.replace(/<[^>]*>?/gm, '');

const getDomainName = (link: string) => {
  try { return new URL(link).hostname.replace('www.', ''); }
  catch { return 'Feed'; }
};

const formatDate = (dateStr?: string) => {
  if (!dateStr) return '';
  try {
    return new Date(dateStr).toLocaleDateString(undefined, {
      month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
    });
  } catch { return dateStr; }
};

type RawItem = Record<string, any>;

type ProcessedItem = RawItem & {
  _key: string;
  _accent: string;
  _domain: string;
  _preview: string;
  _sanitizedContent: string;
  _sanitizedDescription: string;
  _formattedDate: string;
};

const processItem = (item: RawItem): ProcessedItem => {
  const key = item.link || item.title || '';
  const content = item.content || '';
  const plain = stripTags(content);
  return {
    ...item,
    _key: key,
    _accent: stableAccent(key),
    _domain: getDomainName(item.link),
    _preview: plain.length > 160 ? plain.substring(0, 160) + '...' : plain,
    _sanitizedContent: sanitize(content || item.description || ''),
    _sanitizedDescription:
      item.description && item.description !== content ? sanitize(item.description) : '',
    _formattedDate: formatDate(item.published),
  };
};

type FeedCardProps = {
  item: ProcessedItem;
  idx: number;
  isRead: boolean;
  isFocused: boolean;
  onSelect: (item: ProcessedItem, idx: number) => void;
};

const FeedCard = memo(({ item, idx, isRead, isFocused, onSelect }: FeedCardProps) => (
  <Card
    className={`feed-card glass-card flex flex-col cursor-pointer border-t-[3px] group
      ${isRead ? 'opacity-50 hover:opacity-90' : ''}
      ${isFocused ? 'ring-2 ring-primary/65 shadow-xl scale-[1.01]' : 'hover:-translate-y-0.5 hover:shadow-lg'}
    `}
    style={{ borderTopColor: item._accent, animationDelay: `${idx * 20}ms` }}
    onClick={() => onSelect(item, idx)}
  >
    <CardHeader className="pb-2 pt-4 px-4">
      <div className="flex items-start justify-between gap-3 mb-2">
        <span
          className="inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-bold tracking-widest uppercase border leading-none"
          style={{ borderColor: `${item._accent}40`, backgroundColor: `${item._accent}15`, color: item._accent }}
        >
          {item._domain}
        </span>
        {item._formattedDate && (
          <span className="text-[10px] text-muted-foreground flex items-center gap-1 shrink-0">
            <Calendar className="h-2.5 w-2.5" />
            {item._formattedDate}
          </span>
        )}
      </div>
      <CardTitle className="text-sm font-semibold leading-snug line-clamp-2 group-hover:text-primary transition-colors duration-150">
        {item.title}
      </CardTitle>
    </CardHeader>
    <CardContent className="flex-grow pt-0 flex flex-col justify-between px-4 pb-4">
      <p className="text-xs text-muted-foreground leading-relaxed line-clamp-3 mb-3">
        {item._preview}
      </p>
      <div className="flex items-center justify-between text-xs pt-2.5 border-t border-border/20">
        <span className="font-medium inline-flex items-center gap-1.5" style={{ color: item._accent }}>
          <BookOpen className="h-3 w-3" /> Read preview
        </span>
        <a
          href={item.link}
          className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors font-medium"
          target="_blank"
          rel="noopener noreferrer"
          onClick={(e) => e.stopPropagation()}
        >
          Source <ExternalLink className="h-2.5 w-2.5" />
        </a>
      </div>
    </CardContent>
  </Card>
));

const Feeds = ({ data: initialData, error: initialError }) => {
  const [data, setData] = useState<ProcessedItem[]>(
    Array.isArray(initialData) ? initialData.map(processItem) : []
  );
  const [error, setError] = useState(initialError);
  const [page, setPage] = useState(1);
  const [query, setQuery] = useState('');
  const [itemsPerPage, setItemsPerPage] = useState(16);
  const [selectedItem, setSelectedItem] = useState<ProcessedItem | null>(null);
  const [readItems, setReadItems] = useState<Set<string>>(new Set());
  const [focusedIndex, setFocusedIndex] = useState<number | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  useEffect(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) setReadItems(new Set(JSON.parse(stored)));
    } catch {}
  }, []);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 3500);
    return () => clearTimeout(t);
  }, [toast]);

  useEffect(() => {
    const refresh = async () => {
      try {
        const res = await fetch('/api/feeds', { headers: { 'Accept': 'application/json' } });
        if (!res.ok) return;
        const fresh: RawItem[] = await res.json();
        if (!Array.isArray(fresh) || fresh.length === 0) return;
        setError(undefined);
        setData(prev => {
          const seen = new Set(prev.map(item => item._key));
          const newItems = fresh.filter(item => !seen.has(item.link || item.title || ''));
          if (prev.length === 0) return fresh.map(processItem);
          if (newItems.length === 0) return prev;
          setToast(`${newItems.length} new item${newItems.length !== 1 ? 's' : ''}`);
          return [...newItems.map(processItem), ...prev];
        });
      } catch {}
    };

    const id = setInterval(refresh, error ? RECOVERY_MS : REFRESH_MS);
    return () => clearInterval(id);
  }, [error]);

  const markRead = useCallback((key: string) => {
    setReadItems(prev => {
      const next = new Set(prev);
      next.add(key);
      try { localStorage.setItem(STORAGE_KEY, JSON.stringify([...next])); } catch {}
      return next;
    });
  }, []);

  const handleSelect = useCallback((item: ProcessedItem, idx: number) => {
    setFocusedIndex(idx);
    setSelectedItem(item);
    markRead(item._key);
  }, [markRead]);

  const handleSearch = (value: string) => { setQuery(value); setPage(1); setFocusedIndex(null); };
  const handlePageChange = (newPage: number) => {
    setPage(newPage);
    setFocusedIndex(null);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const filteredData = query.trim()
    ? data.filter(item => {
        const q = query.toLowerCase();
        return (item.title || '').toLowerCase().includes(q) || item._preview.toLowerCase().includes(q);
      })
    : data;

  const totalPages = Math.max(1, Math.ceil(filteredData.length / itemsPerPage));
  const startIndex = (page - 1) * itemsPerPage;
  const paginatedData = filteredData.slice(startIndex, startIndex + itemsPerPage);
  const hasMorePages = page < totalPages;
  const unreadCount = data.filter(item => !readItems.has(item._key)).length;

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
      if (e.key === 'j') {
        setFocusedIndex(i => Math.min((i ?? -1) + 1, paginatedData.length - 1));
      } else if (e.key === 'k') {
        setFocusedIndex(i => Math.max((i ?? 1) - 1, 0));
      } else if (e.key === 'o') {
        if (focusedIndex !== null) {
          const item = paginatedData[focusedIndex];
          if (item?.link) window.open(item.link, '_blank', 'noopener,noreferrer');
        }
      } else if (e.key === 'Enter') {
        if (focusedIndex !== null) {
          const item = paginatedData[focusedIndex];
          if (item) handleSelect(item, focusedIndex);
        }
      }
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [paginatedData, focusedIndex]);

  if (error && data.length === 0) return (
    <div className="p-8 text-center space-y-2">
      <p className="text-red-500 font-semibold">Error: {error}</p>
      <p className="text-sm text-muted-foreground">Retrying every 10 seconds...</p>
    </div>
  );

  if (data.length === 0) return (
    <div className="p-8 text-center space-y-2">
      <p className="text-muted-foreground">No feeds available yet.</p>
      <p className="text-sm text-muted-foreground">
        The poller may still be starting up, or no feeds have been configured.{' '}
        <a href="/config" className="underline hover:text-foreground transition-colors font-medium">Add RSS feeds</a> to get started.
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

      <div className="w-full max-w-screen-2xl mx-auto">
        <div className="mb-8 text-center">
          <h1 className="text-4xl md:text-5xl font-bold mb-3">Latest Feeds</h1>
          {unreadCount > 0 && (
            <span className="inline-flex items-center gap-1.5 rounded-full bg-primary/10 px-3 py-1 text-xs font-semibold text-primary">
              <span className="h-1.5 w-1.5 rounded-full bg-primary animate-pulse" />
              {unreadCount} unread
            </span>
          )}
        </div>

        <div className="mb-8 flex flex-col sm:flex-row items-stretch sm:items-center gap-3 max-w-2xl mx-auto">
          <label
            htmlFor="feed-search"
            className="flex-1 flex items-center gap-2 h-10 rounded-lg border border-border/60 bg-card/60 backdrop-blur-md px-3 shadow-sm focus-within:ring-2 focus-within:ring-primary/50 transition-all"
          >
            <Search className="h-4 w-4 text-muted-foreground shrink-0" />
            <input
              type="search"
              id="feed-search"
              placeholder="Search feeds… (j/k to navigate, Enter to open)"
              value={query}
              onInput={(e) => handleSearch((e.target as HTMLInputElement).value)}
              className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
          </label>

          <label
            htmlFor="items-per-page"
            className="shrink-0 flex items-center gap-2 h-10 rounded-lg border border-border/60 bg-card/60 backdrop-blur-md px-3 shadow-sm focus-within:ring-2 focus-within:ring-primary/50 transition-all cursor-pointer"
          >
            <select
              id="items-per-page"
              value={itemsPerPage}
              onChange={(e) => { setItemsPerPage(Number((e.target as HTMLSelectElement).value)); setPage(1); }}
              className="bg-transparent text-sm outline-none cursor-pointer appearance-none pr-1"
            >
              {[16, 32, 64].map(n => <option key={n} value={n} className="bg-background">{n} per page</option>)}
            </select>
            <ChevronDown className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
          </label>
        </div>

        {filteredData.length === 0 && (
          <div className="p-8 text-center text-muted-foreground">No results for "{query}".</div>
        )}

        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-5">
          {paginatedData.map((item, idx) => (
            <FeedCard
              key={item._key}
              item={item}
              idx={idx}
              isRead={readItems.has(item._key)}
              isFocused={focusedIndex === idx}
              onSelect={handleSelect}
            />
          ))}
        </div>
      </div>

      {filteredData.length > 0 && (
        <div className="container mx-auto mt-10 flex justify-center items-center gap-4">
          <Button variant="outline" onClick={() => handlePageChange(Math.max(1, page - 1))} disabled={page === 1}>
            ← Previous
          </Button>
          <span className="text-sm text-muted-foreground tabular-nums font-medium">
            Page {page} of {totalPages}
          </span>
          <Button variant="outline" onClick={() => handlePageChange(Math.min(page + 1, totalPages))} disabled={!hasMorePages}>
            Next →
          </Button>
        </div>
      )}

      <Drawer
        isOpen={!!selectedItem}
        onClose={() => setSelectedItem(null)}
        title={selectedItem?.title}
        accentColor={selectedItem?._accent}
      >
        {selectedItem && (
          <div className="space-y-6">
            {/* Meta row */}
            <div className="flex items-center justify-between gap-4">
              <div className="flex items-center gap-3">
                <span
                  className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-bold uppercase tracking-wider border"
                  style={{
                    borderColor: `${selectedItem._accent}40`,
                    backgroundColor: `${selectedItem._accent}12`,
                    color: selectedItem._accent,
                  }}
                >
                  <Compass className="h-3 w-3" />
                  {selectedItem._domain}
                </span>
                {selectedItem._formattedDate && (
                  <span className="text-xs text-muted-foreground flex items-center gap-1.5">
                    <Calendar className="h-3 w-3" />
                    {selectedItem._formattedDate}
                  </span>
                )}
              </div>
              <a
                href={selectedItem.link}
                className="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-semibold transition-all hover:brightness-110 active:scale-95"
                style={{ backgroundColor: selectedItem._accent, color: '#fff' }}
                target="_blank"
                rel="noopener noreferrer"
              >
                Go to original <ExternalLink className="h-3 w-3" />
              </a>
            </div>

            {/* Description lede */}
            {selectedItem._sanitizedDescription && (
              <div
                className="text-base font-medium italic leading-relaxed border-l-[3px] pl-4 py-0.5 text-muted-foreground"
                style={{ borderColor: selectedItem._accent }}
                dangerouslySetInnerHTML={{ __html: selectedItem._sanitizedDescription }}
              />
            )}

            {/* Accent divider */}
            <div
              className="h-px w-12"
              style={{ background: selectedItem._accent }}
            />

            {/* Body */}
            <div
              className="prose dark:prose-invert max-w-none leading-relaxed"
              dangerouslySetInnerHTML={{ __html: selectedItem._sanitizedContent }}
            />
          </div>
        )}
      </Drawer>
    </div>
  );
};

export default Feeds;
