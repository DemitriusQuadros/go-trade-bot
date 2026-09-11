import React, { useState, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useStrategies } from '@/hooks/queries';
import { api } from '@/api/client';
import { Strategy, StrategyMode, StrategyStatus } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { LoadingScreen } from '@/components/ui/Spinner';
import {
  Plus,
  Play,
  Pause,
  RefreshCw,
  Send,
  X,
  Code2,
} from 'lucide-react';

export function Strategies() {
  const navigate = useNavigate();
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const { data: strategies = [], refetch: refetchStrategies, isLoading: isStrategiesLoading } = useStrategies();
  const [modeFilter, setModeFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');

  // Confirm dialog state
  const [confirmDialog, setConfirmDialog] = useState<{
    isOpen: boolean;
    title: string;
    message: string;
    isDangerous: boolean;
    onConfirm: () => void;
  }>({
    isOpen: false,
    title: '',
    message: '',
    isDangerous: false,
    onConfirm: () => {},
  });

  // Action status message
  const [actionMessage, setActionMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const filteredStrategies = useMemo(() => {
    if (!strategies) return [];
    return strategies.filter((s) => {
      const matchStatus = statusFilter === 'all' || s.status === statusFilter;
      const matchMode = modeFilter === 'all' || s.mode === modeFilter;
      const matchSearch =
        searchQuery === '' ||
        s.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        s.strategy_name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        s.monitored_symbols.some((sym) => sym.toLowerCase().includes(searchQuery.toLowerCase()));
      return matchStatus && matchMode && matchSearch;
    });
  }, [strategies, statusFilter, modeFilter, searchQuery]);

  const handleToggleStatus = async (strat: Strategy) => {
    const newStatus: StrategyStatus = strat.status === 'disabled' ? 'productive' : 'disabled';
    try {
      await api.patchStrategyStatus(strat.id, newStatus);
      setActionMessage({ type: 'success', text: `Strategy #${strat.id} set to ${newStatus}` });
      refetchStrategies();
    } catch (err: any) {
      setActionMessage({ type: 'error', text: err.message || 'Failed to update status' });
    }
  };

  const handleModeChange = (strat: Strategy, newMode: StrategyMode) => {
    if (newMode === 'live') {
      setConfirmDialog({
        isOpen: true,
        title: 'Confirm Switch to LIVE Trading',
        message: `Are you sure you want to enable REAL LIVE execution for "${strat.name}"? Real orders and capital will be committed on the exchange.`,
        isDangerous: true,
        onConfirm: async () => {
          setConfirmDialog((prev) => ({ ...prev, isOpen: false }));
          await executeModeChange(strat.id, newMode);
        },
      });
    } else {
      executeModeChange(strat.id, newMode);
    }
  };

  const executeModeChange = async (id: number, mode: StrategyMode) => {
    try {
      await api.patchStrategyMode(id, mode);
      setActionMessage({ type: 'success', text: `Strategy #${id} switched to ${mode} mode` });
      refetchStrategies();
    } catch (err: any) {
      setActionMessage({ type: 'error', text: err.message || 'Failed to update mode' });
    }
  };

  const handleEnqueue = async () => {
    try {
      await api.enqueueStrategy();
      setActionMessage({ type: 'success', text: 'All active strategies enqueued for tick evaluation' });
    } catch (err: any) {
      setActionMessage({ type: 'error', text: err.message || 'Failed to trigger enqueue' });
    }
  };

  if (isStrategiesLoading && !strategies) {
    return <LoadingScreen message="Loading strategies..." />;
  }

  return (
    <div className="container-custom space-y-6">
      {/* Header */}
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-green-500 flex items-center gap-2">
            Strategy Management
          </h1>
          <p className="text-xs text-green-700 mt-0.5">
            Configure algorithmic strategies, operating parameters & execution modes
          </p>
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={handleEnqueue}
            className="btn btn-secondary text-xs flex items-center gap-1.5"
            title="Force immediate worker execution pass"
          >
            <Send className="w-3.5 h-3.5 text-green-500" />
            <span>Enqueue Tick</span>
          </button>

          <button
            data-walkthrough="new-strategy-btn"
            onClick={() => navigate('/strategies/new')}
            className="btn btn-primary text-xs flex items-center gap-1.5"
          >
            <Plus className="w-4 h-4" />
            <span>New Strategy</span>
          </button>
        </div>
      </div>

      {/* Action status notification */}
      {actionMessage && (
        <div
          className={`p-3 rounded-lg border text-xs flex items-center justify-between ${
            actionMessage.type === 'success'
              ? 'bg-emerald-950/60 border-emerald-800 text-emerald-300'
              : 'bg-rose-950/60 border-rose-800 text-rose-300'
          }`}
        >
          <span>{actionMessage.text}</span>
          <button onClick={() => setActionMessage(null)} className="p-0.5 hover:text-white">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* Filters Bar */}
      <Card>
        <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4">
          <div className="flex-1 max-w-sm">
            <input
              type="text"
              placeholder="Search by name, algorithm, symbol, rules..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="form-input text-xs"
            />
          </div>

          <div className="flex items-center gap-3 flex-wrap">
            <div className="flex items-center gap-2">
              <span className="text-xs text-green-700 font-medium">Status:</span>
              <select
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value)}
                className="form-select text-xs py-1"
              >
                <option value="all">All Statuses</option>
                <option value="productive">Productive</option>
                <option value="testing">Testing</option>
                <option value="disabled">Disabled</option>
              </select>
            </div>

            <div className="flex items-center gap-2">
              <span className="text-xs text-green-700 font-medium">Mode:</span>
              <select
                value={modeFilter}
                onChange={(e) => setModeFilter(e.target.value)}
                className="form-select text-xs py-1"
              >
                <option value="all">All Modes</option>
                <option value="dryrun">Dry Run</option>
                <option value="paper">Paper</option>
                <option value="live">Live</option>
                <option value="backtest">Backtest</option>
              </select>
            </div>

            <button onClick={() => refetchStrategies()} className="btn btn-secondary text-xs py-1" title="Refresh">
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </Card>

      {/* Strategies List Table */}
      <Card>
        <CardHeader
          title="Configured Strategies"
          subtitle={`Displaying ${filteredStrategies.length} of ${strategies?.length || 0} registered bots`}
        />

        {strategies.length === 0 ? (
          <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg space-y-3">
            <p>No strategies configured.</p>
            <button onClick={() => navigate('/strategies/new')} className="btn btn-primary text-xs">
              + New Strategy
            </button>
          </div>
        ) : filteredStrategies.length === 0 ? (
          <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg">
            No strategies found matching filter criteria.
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-green-900/30">
            <table className="w-full table-fixed border-collapse">
              <thead>
                <tr className="border-b border-green-900/40 bg-green-950/20">
                  <th className="w-[6%] px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wide text-green-700">ID</th>
                  <th className="w-[17%] px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wide text-green-700">Name</th>
                  <th className="w-[12%] px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wide text-green-700">Algorithm</th>
                  <th className="w-[21%] px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wide text-green-700">Monitored Symbols</th>
                  <th className="w-[8%] px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wide text-green-700">Cycle</th>
                  <th className="w-[10%] px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wide text-green-700">Status</th>
                  <th className="w-[12%] px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wide text-green-700">Mode</th>
                  <th className="w-[14%] px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wide text-green-700">Actions</th>
                </tr>
              </thead>
              <tbody>
                {filteredStrategies.map((strat) => (
                  <tr key={strat.id} className="border-b border-green-900/20 last:border-b-0 hover:bg-green-950/10">
                    <td className="px-4 py-3 align-top font-mono text-xs text-green-800">#{strat.id}</td>
                    <td className="px-4 py-3 align-top">
                      <div>
                        <div className="font-semibold text-green-400">{strat.name}</div>
                        {strat.description && (
                          <div className="text-[11px] text-green-700 mt-0.5 line-clamp-1">
                            {strat.description}
                          </div>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <span className="font-semibold text-sm text-green-400">{strat.strategy_name}</span>
                    </td>
                    <td className="px-4 py-3 align-top">
                      {strat.monitored_symbols?.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {strat.monitored_symbols.map((sym) => (
                            <span
                              key={sym}
                              className="px-1.5 py-0.5 rounded bg-green-950/50 border border-green-900/50 text-[11px] font-mono text-green-500"
                            >
                              {sym}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <span className="text-green-900 text-xs">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 align-top">
                      <span className="text-xs text-green-700 font-mono">
                        {strat.cycle}
                        <span className="text-green-900"> min</span>
                      </span>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <StatusBadge status={strat.status} />
                    </td>
                    <td className="px-4 py-3 align-top">
                      <select
                        value={strat.mode}
                        onChange={(e) => handleModeChange(strat, e.target.value as StrategyMode)}
                        className="w-full bg-green-950/20 border border-slate-700 text-xs rounded px-2 py-1 text-green-400 focus:outline-none focus:border-blue-500 font-medium"
                      >
                        <option value="dryrun">Dry Run</option>
                        <option value="paper">Paper</option>
                        <option value="live">LIVE</option>
                        <option value="backtest">Backtest</option>
                      </select>
                    </td>
                    <td className="px-4 py-3 align-top">
                      <div className="flex flex-col items-start gap-1.5">
                        <button
                          onClick={() => navigate(`/strategies/${strat.id}/edit`)}
                          className="btn btn-secondary text-xs py-1 px-2.5 flex items-center gap-1.5 whitespace-nowrap"
                          title="View or edit this strategy's Lua script"
                        >
                          <Code2 className="w-3.5 h-3.5 text-green-600" />
                          <span>View/Edit Script</span>
                        </button>

                        <button
                          onClick={() => handleToggleStatus(strat)}
                          className={`btn text-xs py-1 px-2.5 whitespace-nowrap ${
                            strat.status === 'disabled'
                              ? 'btn-success'
                              : 'btn-secondary text-green-600'
                          }`}
                          title={strat.status === 'disabled' ? 'Enable' : 'Disable'}
                        >
                          {strat.status === 'disabled' ? (
                            <>
                              <Play className="w-3 h-3" />
                              <span>Enable</span>
                            </>
                          ) : (
                            <>
                              <Pause className="w-3 h-3" />
                              <span>Disable</span>
                            </>
                          )}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {/* Live confirmation dialog */}
      <ConfirmDialog
        isOpen={confirmDialog.isOpen}
        title={confirmDialog.title}
        message={confirmDialog.message}
        isDangerous={confirmDialog.isDangerous}
        confirmText="Confirm LIVE Mode"
        onConfirm={confirmDialog.onConfirm}
        onCancel={() => setConfirmDialog((prev) => ({ ...prev, isOpen: false }))}
      />
    </div>
  );
}
