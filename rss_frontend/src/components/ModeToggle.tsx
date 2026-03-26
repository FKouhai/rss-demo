import { useState, useEffect } from 'preact/hooks';
import { Moon, Sun } from 'lucide-preact';

export function ModeToggle() {
  const [isDark, setIsDark] = useState(false);

  useEffect(() => {
    setIsDark(document.documentElement.classList.contains('dark'));
  }, []);

  const toggle = () => {
    const next = !isDark;
    document.documentElement.classList[next ? 'add' : 'remove']('dark');
    setIsDark(next);
  };

  return (
    <button
      onClick={toggle}
      className="inline-flex items-center justify-center rounded-md p-2 h-9 w-9 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring bg-primary text-primary-foreground shadow hover:bg-primary/90"
      aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
    >
      {isDark
        ? <Sun className="h-[1.2rem] w-[1.2rem]" />
        : <Moon className="h-[1.2rem] w-[1.2rem]" />
      }
    </button>
  );
}
