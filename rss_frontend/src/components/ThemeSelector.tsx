import { useState, useEffect } from 'preact/hooks';

const THEMES = [
  { id: 'slate',  label: 'Slate',  color: '#5E6AD2' },
  { id: 'amber',  label: 'Amber',  color: '#D97706' },
  { id: 'purple', label: 'Purple', color: '#7C3AED' },
] as const;

type ThemeId = typeof THEMES[number]['id'];

export function ThemeSelector() {
  const [active, setActive] = useState<ThemeId>('slate');

  useEffect(() => {
    const saved = (localStorage.getItem('color-theme') as ThemeId) || 'slate';
    setActive(saved);
    document.documentElement.setAttribute('data-theme', saved);
  }, []);

  const select = (id: ThemeId) => {
    document.documentElement.setAttribute('data-theme', id);
    localStorage.setItem('color-theme', id);
    setActive(id);
  };

  return (
    <div class="flex items-center gap-2 px-2">
      {THEMES.map(({ id, label, color }) => (
        <button
          key={id}
          onClick={() => select(id)}
          title={label}
          aria-label={`Switch to ${label} theme`}
          class={`h-5 w-5 rounded-full transition-all duration-150 ${
            active === id
              ? 'ring-2 ring-offset-2 ring-offset-background scale-110'
              : 'opacity-60 hover:opacity-100 hover:scale-105'
          }`}
          style={{ backgroundColor: color, ringColor: color }}
        />
      ))}
    </div>
  );
}
