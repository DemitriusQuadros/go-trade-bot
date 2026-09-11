import React from 'react';
import {
  HelpCircle,
  PlayCircle,
  LayoutDashboard,
  Layers,
  Wand2,
  FlaskConical,
  Sliders,
  DollarSign,
  Download,
  Settings,
  ChevronRight,
  BookOpen,
} from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { WALKTHROUGH_STORAGE_KEY } from '@/components/domain/Walkthrough';

export interface HelpSection {
  slug: string;
  title: string;
  icon: React.ReactNode;
  summary: string;
  content: React.ReactNode;
}

export const HELP_SECTIONS: HelpSection[] = [
  {
    slug: 'dashboard',
    title: 'Dashboard & Overview',
    icon: <LayoutDashboard className="w-4 h-4 text-blue-400" />,
    summary: 'High-level real-time performance, active positions, and market pulse.',
    content: (
      <div className="space-y-2 text-xs text-slate-300 leading-relaxed">
        <p>
          The Dashboard provides a consolidated view of your trading operations. It displays real-time price
          tickers streamed via Server-Sent Events (SSE), active signals, aggregate realized and unrealized
          P&L, and recent performance snapshots.
        </p>
        <p>
          Use the mode indicator at the top of the screen to quickly verify whether the engine is operating in
          Dry Run, Paper Trading, or Real Live mode.
        </p>
      </div>
    ),
  },
  {
    slug: 'strategies',
    title: 'Strategies Management',
    icon: <Layers className="w-4 h-4 text-emerald-400" />,
    summary: 'View, enable, disable, and adjust execution modes for your trading bots.',
    content: (
      <div className="space-y-2 text-xs text-slate-300 leading-relaxed">
        <p>
          The Strategies screen lists all registered bots. For each strategy, you can inspect its monitored
          symbols, evaluation cycle, current status (Productive, Testing, Disabled), and operating mode.
        </p>
        <p>
          You can toggle strategy execution on/off instantly or change its mode. Switching any strategy to{' '}
          <strong className="text-white">LIVE</strong> mode prompts a safety confirmation dialog to prevent
          accidental capital exposure.
        </p>
      </div>
    ),
  },
  {
    slug: 'strategy-builder',
    title: 'Strategy Builder Wizard',
    icon: <Wand2 className="w-4 h-4 text-purple-400" />,
    summary: 'Build multi-rule algorithmic trading strategies without writing code.',
    content: (
      <div className="space-y-2 text-xs text-slate-300 leading-relaxed">
        <p>
          The Strategy Builder is a 5-step guided wizard that turns your trading ideas into executable bots:
        </p>
        <ul className="list-disc list-inside space-y-1 pl-2 text-slate-300">
          <li>
            <strong className="text-white">1. Scope & Market:</strong> Strategy name, symbols, and candle cycle.
          </li>
          <li>
            <strong className="text-white">2. Entry Conditions:</strong> Build technical indicator rules (RSI,
            EMA, MACD, Bollinger Bands, Volume) with threshold or crossing logic.
          </li>
          <li>
            <strong className="text-white">3. Sizing & Protection:</strong> Set capital allocation and mandatory
            stop-loss percentage (enforced as real exchange STOP_MARKET orders).
          </li>
          <li>
            <strong className="text-white">4. Position Management:</strong> Optional indicator-based exit rules.
          </li>
          <li>
            <strong className="text-white">5. Review & Test:</strong> Inspect the server-generated plain-English
            summary and deploy as draft, live, or jump directly into Backtesting.
          </li>
        </ul>
      </div>
    ),
  },
  {
    slug: 'backtest',
    title: 'Backtesting Engine',
    icon: <FlaskConical className="w-4 h-4 text-amber-400" />,
    summary: 'Simulate strategies against historical market data with equity curves and Monte Carlo.',
    content: (
      <div className="space-y-2 text-xs text-slate-300 leading-relaxed">
        <p>
          Test any saved strategy on historical candle data. The backtest engine simulates fill slippage,
          exchange trading fees, and execution latency.
        </p>
        <p>
          Detailed reports include Sharpe ratio, max drawdown, win rate, profit factor, equity curves, drawdown
          charts, and 1,000-iteration Monte Carlo stress tests.
        </p>
      </div>
    ),
  },
  {
    slug: 'optimization',
    title: 'Hyperparameter Optimization',
    icon: <Sliders className="w-4 h-4 text-cyan-400" />,
    summary: 'Run grid-search parameter sweeps to discover optimal indicator parameters.',
    content: (
      <div className="space-y-2 text-xs text-slate-300 leading-relaxed">
        <p>
          Sweep across ranges of indicator periods, standard deviations, and thresholds. The optimizer
          evaluates all parameter combinations against historical candles and generates 2D heatmaps to highlight
          profitable and stable parameter clusters.
        </p>
      </div>
    ),
  },
  {
    slug: 'positions',
    title: 'Positions & Execution Log',
    icon: <DollarSign className="w-4 h-4 text-green-400" />,
    summary: 'Track open trading signals, order fills, fees, and broker execution history.',
    content: (
      <div className="space-y-2 text-xs text-slate-300 leading-relaxed">
        <p>
          Inspect all open and closed signal positions with detailed breakdown of entry prices, current market
          prices, stop-loss order IDs, and net profit/loss.
        </p>
      </div>
    ),
  },
  {
    slug: 'candle-import',
    title: 'Candle Import & Schedules',
    icon: <Download className="w-4 h-4 text-blue-400" />,
    summary: 'Download historical kline data and schedule automated background syncs.',
    content: (
      <div className="space-y-2 text-xs text-slate-300 leading-relaxed">
        <p>
          The Candle Import screen lets you batch-download historical market data across multiple symbols and
          timeframes for backtesting.
        </p>
        <p>
          You can also configure automated recurring sync schedules (daily or weekly cron tasks) that the
          background worker executes automatically.
        </p>
      </div>
    ),
  },
  {
    slug: 'settings',
    title: 'Platform Settings & Safety',
    icon: <Settings className="w-4 h-4 text-slate-400" />,
    summary: 'Configure broker API credentials, safety ceilings, and monitoring links.',
    content: (
      <div className="space-y-2 text-xs text-slate-300 leading-relaxed">
        <p>
          Manage your exchange API credentials safely with secret masking. Settings modifications use in-process
          hot swapping (with up to 30-second cycle draining for risk-bearing credential/mode changes).
        </p>
        <p>
          You can also configure webhook URLs for alerts and external Prometheus/Grafana dashboard links.
        </p>
      </div>
    ),
  },
];

export function Help({ onReplayWalkthrough }: { onReplayWalkthrough?: () => void }) {
  const handleReplay = () => {
    localStorage.removeItem(WALKTHROUGH_STORAGE_KEY);
    if (onReplayWalkthrough) {
      onReplayWalkthrough();
    } else {
      window.location.reload();
    }
  };

  const scrollToSection = (slug: string) => {
    const el = document.getElementById(slug);
    if (el) {
      el.scrollIntoView({ behavior: 'smooth' });
    }
  };

  return (
    <div className="max-w-5xl mx-auto space-y-6 pb-20">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-white flex items-center gap-2">
            <BookOpen className="w-6 h-6 text-blue-400" /> Help & Documentation
          </h1>
          <p className="text-xs text-slate-400 mt-1">
            Comprehensive operational guide for trading bots, strategy creation, and platform configuration.
          </p>
        </div>

        <button
          type="button"
          onClick={handleReplay}
          className="px-3.5 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-xs font-semibold text-white shadow flex items-center gap-2 self-start sm:self-auto transition-colors"
        >
          <PlayCircle className="w-4 h-4" />
          <span>Replay Walkthrough</span>
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
        {/* In-page navigation sidebar */}
        <div className="md:col-span-1 space-y-2">
          <div className="p-3 rounded-xl bg-slate-900/80 border border-slate-800 space-y-1 sticky top-6">
            <h3 className="text-xs font-bold text-slate-400 uppercase tracking-wider px-2 py-1">
              Table of Contents
            </h3>
            <nav className="space-y-0.5">
              {HELP_SECTIONS.map((sec) => (
                <button
                  key={sec.slug}
                  type="button"
                  onClick={() => scrollToSection(sec.slug)}
                  className="w-full text-left px-2.5 py-1.5 rounded-lg text-xs font-medium text-slate-300 hover:text-white hover:bg-slate-800 flex items-center justify-between transition-colors group"
                >
                  <span className="flex items-center gap-2 truncate">
                    {sec.icon}
                    <span className="truncate">{sec.title}</span>
                  </span>
                  <ChevronRight className="w-3 h-3 opacity-0 group-hover:opacity-100 text-slate-400 shrink-0" />
                </button>
              ))}
            </nav>
          </div>
        </div>

        {/* Sections Body */}
        <div className="md:col-span-3 space-y-4">
          {HELP_SECTIONS.map((sec) => (
            <Card key={sec.slug} id={sec.slug}>
              <div className="p-5 space-y-3 scroll-mt-6">
                <div className="flex items-center gap-2.5 border-b border-slate-800 pb-3">
                  <div className="p-2 rounded-lg bg-slate-950 border border-slate-800">
                    {sec.icon}
                  </div>
                  <div>
                    <h2 className="text-base font-bold text-white">{sec.title}</h2>
                    <p className="text-xs text-slate-400">{sec.summary}</p>
                  </div>
                </div>

                <div className="pt-1">{sec.content}</div>
              </div>
            </Card>
          ))}
        </div>
      </div>
    </div>
  );
}
