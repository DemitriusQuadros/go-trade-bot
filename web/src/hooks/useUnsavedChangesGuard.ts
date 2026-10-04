import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';

// useUnsavedChangesGuard protects a dirty form from being navigated away
// from. The app uses <BrowserRouter> (not a data router), so react-router's
// useBlocker isn't available; instead this:
//   - registers `beforeunload` for reloads / tab closes / external links, and
//   - intercepts in-app <a href> clicks (sidebar NavLinks, <Link>s) in the
//     document's CAPTURE phase - before React's root listener sees them - and
//     parks the destination until the caller confirms via its own dialog.
// Browser back/forward (popstate) is not intercepted.
//
// Callers render a ConfirmDialog while `pendingPath` is non-null, calling
// confirmLeave() / cancelLeave(). Programmatic navigate() calls (e.g. after
// a successful save) are never intercepted.
export function useUnsavedChangesGuard(dirty: boolean) {
  const navigate = useNavigate();
  const [pendingPath, setPendingPath] = useState<string | null>(null);
  const [bypass, setBypass] = useState(false);
  const active = dirty && !bypass;

  // Once the form is clean again (saved/reset), re-arm the guard.
  useEffect(() => {
    if (!dirty) setBypass(false);
  }, [dirty]);

  useEffect(() => {
    if (!active) return;
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      // Required by some browsers to show the native prompt.
      e.returnValue = '';
    };
    const onClick = (e: MouseEvent) => {
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      const anchor = (e.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null;
      if (!anchor || anchor.target === '_blank' || anchor.hasAttribute('download')) return;
      const url = new URL(anchor.href, window.location.href);
      if (url.origin !== window.location.origin) return;
      const next = url.pathname + url.search + url.hash;
      if (next === window.location.pathname + window.location.search + window.location.hash) return;
      e.preventDefault();
      e.stopPropagation();
      setPendingPath(next);
    };
    window.addEventListener('beforeunload', onBeforeUnload);
    document.addEventListener('click', onClick, true);
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload);
      document.removeEventListener('click', onClick, true);
    };
  }, [active]);

  const confirmLeave = useCallback(() => {
    const path = pendingPath;
    setPendingPath(null);
    if (path) {
      setBypass(true);
      navigate(path);
    }
  }, [pendingPath, navigate]);

  const cancelLeave = useCallback(() => setPendingPath(null), []);

  // For in-page buttons like "Cancel": navigates immediately when clean,
  // otherwise parks `path` so the confirm dialog opens.
  const requestNavigate = useCallback(
    (path: string) => {
      if (active) {
        setPendingPath(path);
        return;
      }
      navigate(path);
    },
    [active, navigate],
  );

  return { pendingPath, confirmLeave, cancelLeave, requestNavigate };
}
