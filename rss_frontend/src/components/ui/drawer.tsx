import { useEffect } from 'preact/hooks';
import { X } from 'lucide-preact';

interface DrawerProps {
  isOpen: boolean;
  onClose: () => void;
  title?: string;
  accentColor?: string;
  children: import('preact').ComponentChildren;
}

export function Drawer({ isOpen, onClose, title, accentColor, children }: DrawerProps) {
  useEffect(() => {
    document.body.style.overflow = isOpen ? 'hidden' : '';
    return () => { document.body.style.overflow = ''; };
  }, [isOpen]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    if (isOpen) window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex justify-center bg-black/50 backdrop-blur-sm transition-opacity duration-200"
      style={{ opacity: isOpen ? 1 : 0, pointerEvents: isOpen ? 'auto' : 'none' }}
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-4xl h-full bg-background flex flex-col shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Accent bar */}
        <div
          className="h-1 w-full shrink-0 transition-colors duration-300"
          style={{ background: accentColor || 'var(--primary)' }}
        />

        {/* Header */}
        <div
          className="shrink-0 px-8 pt-6 pb-5 border-b border-border/40"
          style={{
            background: accentColor
              ? `color-mix(in srgb, ${accentColor} 6%, var(--background))`
              : 'var(--background)',
          }}
        >
          <div className="flex items-start justify-between gap-4">
            <h2 className="text-xl font-bold leading-snug text-foreground">
              {title || 'Article'}
            </h2>
            <button
              onClick={onClose}
              className="shrink-0 mt-0.5 rounded-full p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
              aria-label="Close"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-8 py-8 feeds-content">
          {children}
        </div>
      </div>
    </div>
  );
}
