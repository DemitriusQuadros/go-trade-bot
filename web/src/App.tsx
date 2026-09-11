import React, { useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthGate } from '@/auth/AuthGate';
import { Dashboard } from '@/pages/Dashboard';
import { Strategies } from '@/pages/Strategies';
import { ScriptEditor } from '@/pages/ScriptEditor';
import { ScriptRepl } from '@/pages/ScriptRepl';
import { Positions } from '@/pages/Positions';
import { BacktestLauncher } from '@/pages/BacktestLauncher';
import { BacktestResults } from '@/pages/BacktestResults';
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
            <Route path="/strategies/new" element={<ScriptEditor mode="create" />} />
            <Route path="/strategies/:id/edit" element={<ScriptEditor mode="edit" />} />
            <Route path="/scripts/repl" element={<ScriptRepl />} />
            <Route path="/positions" element={<Positions />} />
            <Route path="/backtest" element={<BacktestLauncher />} />
            <Route path="/backtest/:id" element={<BacktestResults />} />
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
