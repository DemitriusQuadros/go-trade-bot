// Resolves the app's current CSS-variable tokens (index.css) into literal
// color strings a canvas-based chart library can actually use -
// lightweight-charts and hand-rolled canvas/SVG charts take color options
// at creation time, not CSS classes, so they can't just reference
// `hsl(var(--foo))` the way the rest of the app's Tailwind classes do.
// Call this fresh inside a chart's init effect (keyed on useIsDarkMode's
// value) rather than caching it - the underlying token values differ
// between the paper and dark themes.
export interface ChartColors {
  background: string;
  surface: string;
  text: string;
  muted: string;
  border: string;
  primary: string;
  success: string;
  destructive: string;
  warning: string;
}

// Applies alpha to one of the hsl(...) strings getChartColors() returns -
// e.g. for an area chart's fill under its line. Relies on the modern
// `hsl(H S% L% / A)` slash syntax, which every browser this app targets
// (and <canvas> itself) supports.
export function withAlpha(hslColor: string, alpha: number): string {
  return hslColor.replace(/\)$/, ` / ${alpha})`);
}

export function getChartColors(): ChartColors {
  const style = getComputedStyle(document.documentElement);
  const hsl = (name: string) => `hsl(${style.getPropertyValue(name).trim()})`;
  return {
    background: hsl('--background'),
    surface: hsl('--card'),
    text: hsl('--foreground'),
    muted: hsl('--muted-foreground'),
    border: hsl('--border'),
    primary: hsl('--primary'),
    success: hsl('--success'),
    destructive: hsl('--destructive'),
    warning: hsl('--warning'),
  };
}
