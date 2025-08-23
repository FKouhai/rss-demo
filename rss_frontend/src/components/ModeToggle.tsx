// src/components/ModeToggle.jsx
import { useState, useEffect } from 'preact/hooks';
import { Moon, Sun } from 'lucide-preact';

const Button = ({ className = '', children, ...props }) => (
  <button
    className={`inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 bg-primary text-primary-foreground shadow hover:bg-primary/90 p-2 h-9 w-9 ${className}`}
    {...props}
  >
    {children}
  </button>
);

const DropdownMenu = ({ children }) => (
  <div className="relative inline-block text-left">
    {children}
  </div>
);

const DropdownMenuTrigger = ({ children, asChild, ...props }) => {
  if (asChild) {
    return <div {...props}>{children}</div>;
  }
  return <button {...props}>{children}</button>;
};

const DropdownMenuContent = ({ children, align }) => (
  <div className={`absolute z-10 mt-2 w-40 origin-top-right rounded-md border shadow-lg bg-white dark:bg-zinc-900 ring-1 ring-black ring-opacity-5 focus:outline-none ${align === 'end' ? 'right-0' : ''}`}>
    <div className="p-1">
      {children}
    </div>
  </div>
);

const DropdownMenuItem = ({ className = '', children, ...props }) => (
  <div
    className={`p-2 cursor-pointer hover:bg-gray-100 dark:hover:bg-zinc-800 rounded-sm text-sm ${className}`}
    {...props}
  >
    {children}
  </div>
);
export function ModeToggle() {
  const [theme, setThemeState] = useState("theme-light");
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  useEffect(() => {
    const isDarkMode = document.documentElement.classList.contains("dark");
    setThemeState(isDarkMode ? "dark" : "theme-light");
  }, []);

  useEffect(() => {
    const isDark =
      theme === "dark" ||
      (theme === "system" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList[isDark ? "add" : "remove"]("dark");
  }, [theme]);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild onClick={() => setIsMenuOpen(!isMenuOpen)}>
        <Button>
          <Sun className="h-[1.2rem] w-[1.2rem] scale-100 rotate-0 transition-all dark:scale-0 dark:-rotate-90" />
          <Moon className="absolute h-[1.2rem] w-[1.2rem] scale-0 rotate-90 transition-all dark:scale-100 dark:rotate-0" />
          <span className="sr-only">Toggle theme</span>
        </Button>
      </DropdownMenuTrigger>
      {isMenuOpen && (
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={() => { setThemeState("theme-light"); setIsMenuOpen(false); }}>
            Light
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => { setThemeState("dark"); setIsMenuOpen(false); }}>
            Dark
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => { setThemeState("system"); setIsMenuOpen(false); }}>
            System
          </DropdownMenuItem>
        </DropdownMenuContent>
      )}
    </DropdownMenu>
  );
}

