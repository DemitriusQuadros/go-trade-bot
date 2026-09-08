import React from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthGate } from '@/auth/AuthGate';
import { Dashboard } from '@/pages/Dashboard';
import { Strategies } from '@/pages/Strategies';
import { Positions } from '@/pages/Positions';
import { BacktestLauncher } from '@/pages/BacktestLauncher';
import { BacktestResults } from '@/pages/BacktestResults';
import { Optimization } from '@/pages/Optimization';
import { ExecutionLog } from '@/pages/ExecutionLog';
import { AppLayout } from '@/components/layout/AppLayout';

export function App() {
  return (
    <AuthGate>
      <BrowserRouter>
        <AppLayout>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/strategies" element={<Strategies />} />
            <Route path="/positions" element={<Positions />} />
            <Route path="/backtest" element={<BacktestLauncher />} />
            <Route path="/backtest/:id" element={<BacktestResults />} />
            <Route path="/optimization" element={<Optimization />} />
            <Route path="/execution" element={<ExecutionLog />} />
          </Routes>
        </AppLayout>
      </BrowserRouter>
    </AuthGate>
  );
}
