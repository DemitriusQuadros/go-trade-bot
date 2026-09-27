import React from 'react';
import { useLocation } from 'react-router-dom';
import { MiniSidebar } from './MiniSidebar';
import { LogOut, Moon, Sun } from 'lucide-react';
import { clearToken } from '@/api/client';
import { ErrorBoundary } from '@/components/ui/ErrorBoundary';
import { useTheme } from '@/hooks/useTheme';

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const { theme, toggleTheme } = useTheme();
  const handleLogout = () => {
    clearToken();
    window.location.reload();
  };

  return (
    <div className="min-h-screen bg-background text-foreground flex">
      <MiniSidebar />
      <div className="flex-1 flex flex-col ml-14 transition-all duration-300 min-w-0">
        <header className="sticky top-0 z-40 bg-background/90 backdrop-blur-md border-b border-border">
          <div className="h-14 flex items-center justify-end gap-1 px-4">
            <button
              onClick={toggleTheme}
              className="p-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors"
              title={theme === 'dark' ? 'Switch to light (paper) mode' : 'Switch to dark mode'}
              aria-label="Toggle color theme"
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </button>
            <button
              onClick={handleLogout}
              className="flex items-center gap-1.5 px-2 py-2 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 text-xs transition-colors"
              title="Disconnect and clear token"
            >
              <LogOut className="w-4 h-4" />
              <span>Logout</span>
            </button>
          </div>
        </header>
        {/* min-w-0: a flex item's default min-width is auto (its content's
            min-content size), which lets a page with a wide flex-wrap row
            (e.g. WorkbenchShell's header) silently force this whole column
            wider than the viewport instead of actually wrapping - main
            grows past its own flex parent and gets its own horizontal
            scrollbar rather than letting content inside it wrap or shrink. */}
        <main className="flex-1 p-6 overflow-auto min-w-0">
          <ErrorBoundary resetKey={location.pathname}>{children}</ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
