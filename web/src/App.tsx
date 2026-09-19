import React, { useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthGate } from '@/auth/AuthGate';
import { Dashboard } from '@/pages/Dashboard';
import { Strategies } from '@/pages/Strategies';
import { WorkbenchShell } from '@/pages/WorkbenchShell';
import { EditorPane } from '@/pages/EditorPane';
import { ReplPane } from '@/pages/ReplPane';
import { BacktestPane } from '@/pages/BacktestPane';
import { ScriptRepl } from '@/pages/ScriptRepl';
import { Positions } from '@/pages/Positions';
import { BacktestLauncher } from '@/pages/BacktestLauncher';
import { Optimization } from '@/pages/Optimization';
import { ExecutionLog } from '@/pages/ExecutionLog';
import { Settings } from '@/pages/Settings';
import { CandleImport } from '@/pages/CandleImport';
import { Help } from '@/pages/Help';
import { AppLayout } from '@/components/layout/AppLayout';
import { Walkthrough, WALKTHROUGH_STORAGE_KEY } from '@/components/domain/Walkthrough';

export function App() {
  const [showWalkthrough, setShowWalkthrough] = useState(false);

  useEffect(() => {
    const completed = localStorage.getItem(WALKTHROUGH_STORAGE_KEY);
    if (!completed) {
      setShowWalkthrough(true);
    }
  }, []);

  const handleWalkthroughComplete = () => {
    localStorage.setItem(WALKTHROUGH_STORAGE_KEY, 'true');
    setShowWalkthrough(false);
  };

  const handleWalkthroughSkip = () => {
    localStorage.setItem(WALKTHROUGH_STORAGE_KEY, 'true');
    setShowWalkthrough(false);
  };

  const handleReplayWalkthrough = () => {
    setShowWalkthrough(true);
  };

  return (
    <AuthGate>
      <BrowserRouter>
        <AppLayout>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/strategies" element={<Strategies />} />
            <Route path="/strategies/new" element={<WorkbenchShell mode="create" />}>
              <Route index element={<EditorPane />} />
              <Route path="repl" element={<ReplPane />} />
              {/* backtest intentionally omitted for mode="create" - the shell's
                  own tab nav renders the Backtest tab disabled rather than
                  404ing on an unmatched nested path if a stale link is followed. */}
            </Route>
            <Route path="/strategies/:id/edit" element={<WorkbenchShell mode="edit" />}>
              <Route index element={<EditorPane />} />
              <Route path="repl" element={<ReplPane />} />
              <Route path="backtest" element={<BacktestPane />} />
              <Route path="backtest/:runId" element={<BacktestPane />} />
            </Route>
            <Route path="/scripts/repl" element={<ScriptRepl />} />
            <Route path="/positions" element={<Positions />} />
            <Route path="/backtest" element={<BacktestLauncher />} />
            <Route path="/optimization" element={<Optimization />} />
            <Route path="/execution" element={<ExecutionLog />} />
            <Route path="/candles" element={<CandleImport />} />
            <Route path="/settings" element={<Settings />} />
            <Route
              path="/help"
              element={<Help onReplayWalkthrough={handleReplayWalkthrough} />}
            />
          </Routes>
        </AppLayout>
        {showWalkthrough && (
          <Walkthrough
            onComplete={handleWalkthroughComplete}
            onSkip={handleWalkthroughSkip}
          />
        )}
      </BrowserRouter>
    </AuthGate>
  );
}
