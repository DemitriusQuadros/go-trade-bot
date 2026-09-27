import React, { useState, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useStrategies } from '@/hooks/queries';
import { api } from '@/api/client';
import { Strategy, StrategyMode, StrategyStatus } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { LoadingScreen } from '@/components/ui/Spinner';
import { DropdownMenu } from '@/components/ui/DropdownMenu';
import {
  Plus,
  Play,
  Pause,
  RefreshCw,
  Send,
  X,
  Code2,
  Bot,
  Trash2,
} from 'lucide-react';

export function Strategies() {
  const navigate = useNavigate();
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const { data: strategies = [], refetch: refetchStrategies, isLoading: isStrategiesLoading } = useStrategies();
  const [modeFilter, setModeFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');

  // Confirm dialog state. confirmText was previously hardcoded to "Confirm
  // LIVE Mode" at the single shared <ConfirmDialog> render site below -
  // correct for the one action that used this dialog at the time (the
  // live-mode switch), but wrong for every other action that opens it
  // (delete, now) since the button label never changed to match. Making it
  // part of this state, set per-action, is the actual fix.
  const [confirmDialog, setConfirmDialog] = useState<{
    isOpen: boolean;
    title: string;
    message: string;
    confirmText: string;
    isDangerous: boolean;
    onConfirm: () => void;
  }>({
    isOpen: false,
    title: '',
    message: '',
    confirmText: 'Confirm',
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
        confirmText: 'Confirm LIVE Mode',
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

  // Permanently deletes the strategy and everything that references it
  // (signals, orders, executions, backtests, optimization runs,
  // performance snapshots, script history, agent chat history) - no undo.
  // The backend independently blocks a productive strategy or one with
  // open positions (409), surfaced here as an error message rather than
  // relying on this confirmation alone to prevent an unsafe delete.
  const handleDelete = (strat: Strategy) => {
    setConfirmDialog({
      isOpen: true,
      title: 'Permanently Delete Strategy',
      message: `Delete "${strat.name}" (#${strat.id})? This permanently removes it and ALL related data - signals, orders, backtests, executions, performance history, script versions, and agent chat history. This cannot be undone.`,
      confirmText: 'Delete Permanently',
      isDangerous: true,
      onConfirm: async () => {
        setConfirmDialog((prev) => ({ ...prev, isOpen: false }));
        try {
          await api.deleteStrategy(strat.id);
          setActionMessage({ type: 'success', text: `Strategy #${strat.id} deleted` });
          refetchStrategies();
        } catch (err: any) {
          setActionMessage({ type: 'error', text: err.message || 'Failed to delete strategy' });
        }
      },
    });
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
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            Strategy Management
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            Configure algorithmic strategies, operating parameters & execution modes
          </p>
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={handleEnqueue}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs flex items-center gap-1.5 px-3 py-1.5"
            title="Force immediate worker execution pass"
          >
            <Send className="w-3.5 h-3.5 text-primary" />
            <span>Enqueue Tick</span>
          </button>

          <button
            data-walkthrough="new-strategy-btn"
            onClick={() => navigate('/strategies/new')}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
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
              ? 'bg-success/15 border-success/40 text-foreground'
              : 'bg-destructive/15 border-destructive/40 text-foreground'
          }`}
        >
          <span>{actionMessage.text}</span>
          <button onClick={() => setActionMessage(null)} className="p-0.5 text-muted-foreground hover:text-foreground">
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
              <span className="text-xs text-muted-foreground font-medium">Status:</span>
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
              <span className="text-xs text-muted-foreground font-medium">Mode:</span>
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

            <button
              onClick={() => refetchStrategies()}
              className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs p-1.5"
              title="Refresh"
            >
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
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg space-y-3">
            <p>No strategies configured.</p>
            <button
              onClick={() => navigate('/strategies/new')}
              className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs px-3 py-1.5 font-bold"
            >
              + New Strategy
            </button>
          </div>
        ) : filteredStrategies.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg">
            No strategies found matching filter criteria.
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full border-collapse">
              <thead>
                <tr className="border-b border-border bg-secondary/40">
                  <th className="px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Strategy</th>
                  <th className="px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Symbols</th>
                  <th className="px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Status</th>
                  <th className="px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Mode</th>
                  <th className="w-10 px-2 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {filteredStrategies.map((strat) => (
                  <tr
                    key={strat.id}
                    onClick={() => navigate(`/strategies/${strat.id}/edit`)}
                    className="border-b border-border last:border-b-0 hover:bg-accent/40 cursor-pointer transition-colors"
                  >
                    <td className="px-4 py-3 align-top max-w-xs">
                      <div className="font-semibold text-foreground truncate">{strat.name}</div>
                      <div className="text-[11px] text-muted-foreground mt-0.5 flex items-center gap-1.5 font-mono">
                        <span>#{strat.id}</span>
                        <span className="text-muted-foreground/50">·</span>
                        <span>{strat.strategy_name}</span>
                        <span className="text-muted-foreground/50">·</span>
                        <span>{strat.cycle}m</span>
                      </div>
                      {strat.description && (
                        <div className="text-[11px] text-muted-foreground/80 mt-0.5 line-clamp-1">{strat.description}</div>
                      )}
                    </td>
                    <td className="px-4 py-3 align-top">
                      {strat.monitored_symbols?.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {strat.monitored_symbols.map((sym) => (
                            <span
                              key={sym}
                              className="px-1.5 py-0.5 rounded bg-secondary border border-border text-[11px] font-mono text-foreground"
                            >
                              {sym}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <span className="text-muted-foreground text-xs">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 align-top">
                      <StatusBadge status={strat.status} />
                    </td>
                    <td className="px-4 py-3 align-top" onClick={(e) => e.stopPropagation()}>
                      <select
                        value={strat.mode}
                        onChange={(e) => handleModeChange(strat, e.target.value as StrategyMode)}
                        className="bg-secondary border border-border text-xs rounded px-2 py-1 text-foreground focus:outline-none focus:border-primary font-medium"
                      >
                        <option value="dryrun">Dry Run</option>
                        <option value="paper">Paper</option>
                        <option value="live">LIVE</option>
                        <option value="backtest">Backtest</option>
                      </select>
                    </td>
                    <td className="px-2 py-3 align-top text-right" onClick={(e) => e.stopPropagation()}>
                      <DropdownMenu
                        label={`Actions for ${strat.name}`}
                        items={[
                          {
                            label: 'View / Edit Script',
                            icon: <Code2 />,
                            onClick: () => navigate(`/strategies/${strat.id}/edit`),
                          },
                          {
                            label: 'Agent History',
                            icon: <Bot />,
                            onClick: () => navigate(`/activity?tab=agent&strategy_id=${strat.id}`),
                          },
                          {
                            label: strat.status === 'disabled' ? 'Enable' : 'Disable',
                            icon: strat.status === 'disabled' ? <Play /> : <Pause />,
                            onClick: () => handleToggleStatus(strat),
                          },
                          {
                            label: 'Delete',
                            icon: <Trash2 />,
                            onClick: () => handleDelete(strat),
                            destructive: true,
                            separatorBefore: true,
                          },
                        ]}
                      />
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
        confirmText={confirmDialog.confirmText}
        onConfirm={confirmDialog.onConfirm}
        onCancel={() => setConfirmDialog((prev) => ({ ...prev, isOpen: false }))}
      />
    </div>
  );
}
