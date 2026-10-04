import { useEffect, useState } from 'react';

// Charts (lightweight-charts, hand-rolled canvas/SVG) take literal color
// strings at creation time - they can't read CSS custom properties the
// way the rest of the app's Tailwind classes do, so they need to know
// explicitly when the theme flips to rebuild themselves with fresh
// colors. A MutationObserver on <html>'s class, rather than importing
// useTheme.ts directly, so this reacts correctly no matter where the
// toggle was actually clicked (useTheme's own state is local to whichever
// component calls it, not shared context).
export function useIsDarkMode(): boolean {
  const [isDark, setIsDark] = useState(() => document.documentElement.classList.contains('dark'));

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'));
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
    return () => observer.disconnect();
  }, []);

  return isDark;
}
