import React from 'react';
import { AlertTriangle } from 'lucide-react';

interface ErrorBoundaryProps {
  children: React.ReactNode;
  /** Reset the boundary whenever this value changes (e.g. the current route path). */
  resetKey?: string;
}

interface ErrorBoundaryState {
  error: Error | null;
}

/**
 * Catches rendering/lifecycle exceptions in its subtree (e.g. a chart
 * library throwing on malformed data) so they degrade to an inline error
 * message instead of unmounting the whole app to a blank screen. Before
 * this existed, no error boundary was present anywhere in the app - a
 * single bad render in any page component blanked the entire SPA shell,
 * sidebar included, with no way to recover except a manual URL change.
 */
export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // eslint-disable-next-line no-console
    console.error('ErrorBoundary caught a rendering error:', error, info.componentStack);
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps) {
    if (this.state.error && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ error: null });
    }
  }

  render() {
    if (this.state.error) {
      return (
        <div className="flex items-start gap-3 p-6 m-4 rounded-lg border border-red-900/50 bg-red-950/20 text-red-400">
          <AlertTriangle className="w-5 h-5 shrink-0 mt-0.5" />
          <div className="space-y-1">
            <p className="font-semibold">Something went wrong rendering this page.</p>
            <p className="text-xs text-red-500/80">{this.state.error.message}</p>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
