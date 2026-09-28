import React, { useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthGate } from '@/auth/AuthGate';
import { Dashboard } from '@/pages/Dashboard';
import { Strategies } from '@/pages/Strategies';
import { WorkbenchShell } from '@/pages/WorkbenchShell';
import { EditorPane } from '@/pages/EditorPane';
import { ReplPane } from '@/pages/ReplPane';
import { BacktestPane } from '@/pages/BacktestPane';
import { BacktestRuns } from '@/pages/BacktestRuns';
import { Activity } from '@/pages/Activity';
import { Optimization } from '@/pages/Optimization';
import { AgentCopilotWidget } from '@/components/domain/AgentCopilotWidget';
import { Settings } from '@/pages/Settings';
import { CandleImport } from '@/pages/CandleImport';
import { Help } from '@/pages/Help';
import { AppLayout } from '@/components/layout/AppLayout';
import { Walkthrough, WALKTHROUGH_STORAGE_KEY } from '@/components/domain/Walkthrough';
import { EditorBridgeProvider } from '@/context/EditorBridgeContext';
import { ToastProvider } from '@/context/ToastContext';
import { Agents } from '@/pages/Agents';
import { AgentEditor } from '@/pages/AgentEditor';
import { AgentRuns } from '@/pages/AgentRuns';
import { AgentReports } from '@/pages/AgentReports';
import { AgentReportDetail } from '@/pages/AgentReportDetail';
import { Proposals } from '@/pages/Proposals';
import { ProposalDetail } from '@/pages/ProposalDetail';

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
      <EditorBridgeProvider>
      <ToastProvider>
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
            {/* Script REPL and the Backtest launcher used to be standalone
                pages here - both duplicated a per-strategy Workbench tab
                (REPL, Backtest) instead of being that tab. /scripts/repl is
                gone outright (open a new strategy's REPL tab instead);
                /backtest is now BacktestRuns, a pure cross-strategy run
                history browser with no launch form of its own. */}
            <Route path="/backtest" element={<BacktestRuns />} />
            <Route path="/optimization" element={<Optimization />} />
            {/* Agents platform (A-03). Static segments (new, reports) outrank
                :id in react-router's ranking, so /agents/reports never
                resolves to the editor. All of these are client-side only -
                cmd/api's SPA fallback serves index.html for them on a hard
                refresh (every backend route lives under /api). */}
            <Route path="/agents" element={<Agents />} />
            <Route path="/agents/new" element={<AgentEditor key="new" />} />
            <Route path="/agents/reports" element={<AgentReports />} />
            <Route path="/agents/reports/:id" element={<AgentReportDetail />} />
            {/* Phase B (B-02): proposals inbox + detail. /agents/proposals/:id
                is the webhook deep-link target - same hard-refresh story as
                reports above (static "proposals" segment outranks :id). */}
            <Route path="/agents/proposals" element={<Proposals />} />
            <Route path="/agents/proposals/:id" element={<ProposalDetail />} />
            <Route path="/agents/:id" element={<AgentEditor />} />
            <Route path="/agents/:id/runs" element={<AgentRuns />} />
            {/* Positions, Execution Log, and Agent History used to be three
                separate pages - all three are filtered views over the same
                object (one strategy's trading activity), now tabs on one
                Activity page instead. /agent (standalone full-page chat) is
                gone too - replaced by the floating AgentCopilotWidget mounted
                below, alongside these Routes. */}
            <Route path="/activity" element={<Activity />} />
            <Route path="/candles" element={<CandleImport />} />
            <Route path="/settings" element={<Settings />} />
            <Route
              path="/help"
              element={<Help onReplayWalkthrough={handleReplayWalkthrough} />}
            />
          </Routes>
        </AppLayout>
        <AgentCopilotWidget />
        {showWalkthrough && (
          <Walkthrough
            onComplete={handleWalkthroughComplete}
            onSkip={handleWalkthroughSkip}
          />
        )}
      </ToastProvider>
      </EditorBridgeProvider>
      </BrowserRouter>
    </AuthGate>
  );
}
