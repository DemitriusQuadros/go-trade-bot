import { useStrategies } from '@/hooks/queries';
import React, { useState, useMemo } from 'react';
import { api } from '@/api/client';
import { Strategy, StrategyCreateRequest, StrategyMode, StrategyStatus } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { ModeBadge } from '@/components/ui/ModeBadge';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { LoadingScreen } from '@/components/ui/Spinner';
import {
  Plus,
  Play,
  Pause,
  Edit2,
  RefreshCw,
  Sliders,
  Send,
  Check,
  X,
  AlertCircle,
} from 'lucide-react';

export function Strategies() {
  

  const [statusFilter, setStatusFilter] = useState<string>('all');
  const { data: strategies = [], refetch: refetchStrategies, isLoading: isStrategiesLoading } = useStrategies();
  const [modeFilter, setModeFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');

  // Modal states
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingStrategy, setEditingStrategy] = useState<Strategy | null>(null);

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
            onClick={() => {
              setEditingStrategy(null);
              setIsModalOpen(true);
            }}
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
              placeholder="Search by name, algorithm, symbol..."
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

        {filteredStrategies.length === 0 ? (
          <div className="p-8 text-center text-xs text-green-800 bg-black/40 rounded-lg">
            No strategies found matching filter criteria.
          </div>
        ) : (
          <div className="table-container">
            <table className="table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Name</th>
                  <th>Algorithm</th>
                  <th>Monitored Symbols</th>
                  <th>Cycle</th>
                  <th>Status</th>
                  <th>Mode</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {filteredStrategies.map((strat) => (
                  <tr key={strat.id}>
                    <td className="font-mono text-green-800">#{strat.id}</td>
                    <td>
                      <div>
                        <div className="font-semibold text-green-400">{strat.name}</div>
                        {strat.description && (
                          <div className="text-[11px] text-green-700 mt-0.5 line-clamp-1">
                            {strat.description}
                          </div>
                        )}
                      </div>
                    </td>
                    <td className="font-mono text-xs text-green-600">{strat.strategy_name}</td>
                    <td className="font-mono text-xs text-green-600">
                      {strat.monitored_symbols?.length > 0
                        ? strat.monitored_symbols.join(', ')
                        : '—'}
                    </td>
                    <td className="text-xs text-green-700">{strat.cycle} min</td>
                    <td>
                      <StatusBadge status={strat.status} />
                    </td>
                    <td>
                      <select
                        value={strat.mode}
                        onChange={(e) => handleModeChange(strat, e.target.value as StrategyMode)}
                        className="bg-green-950/20 border border-slate-700 text-xs rounded px-2 py-1 text-green-400 focus:outline-none focus:border-blue-500 font-medium"
                      >
                        <option value="dryrun">Dry Run</option>
                        <option value="paper">Paper</option>
                        <option value="live">LIVE</option>
                        <option value="backtest">Backtest</option>
                      </select>
                    </td>
                    <td>
                      <div className="flex items-center gap-2">
                        <button
                          onClick={() => handleToggleStatus(strat)}
                          className={`btn text-xs py-1 px-2.5 ${
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

                        <button
                          onClick={() => {
                            setEditingStrategy(strat);
                            setIsModalOpen(true);
                          }}
                          className="btn btn-secondary text-xs py-1 px-2"
                          title="Edit configuration"
                        >
                          <Edit2 className="w-3 h-3 text-green-600" />
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

      {/* Create / Edit Strategy Modal */}
      {isModalOpen && (
        <StrategyFormModal
          strategy={editingStrategy}
          onClose={() => {
            setIsModalOpen(false);
            setEditingStrategy(null);
          }}
          onSaved={() => {
            setIsModalOpen(false);
            setEditingStrategy(null);
            refetchStrategies();
          }}
        />
      )}

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

function StrategyFormModal({
  strategy,
  onClose,
  onSaved,
}: {
  strategy: Strategy | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const isEditing = !!strategy;

  const [name, setName] = useState(strategy?.name || '');
  const [description, setDescription] = useState(strategy?.description || '');
  const [strategyName, setStrategyName] = useState(strategy?.strategy_name || 'grid');
  const [status, setStatus] = useState<StrategyStatus>(strategy?.status || 'testing');
  const [mode, setMode] = useState<StrategyMode>(strategy?.mode || 'dryrun');
  const [symbols, setSymbols] = useState(strategy?.monitored_symbols?.join(', ') || 'BTCUSDT');
  const [cycle, setCycle] = useState(strategy?.cycle || 5);
  const [configJson, setConfigJson] = useState(
    strategy?.configuration
      ? JSON.stringify(strategy.configuration, null, 2)
      : '{\n  "grid_levels": 10,\n  "grid_spacing_pct": 0.5\n}'
  );
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    let parsedConfig: Record<string, unknown> = {};
    try {
      parsedConfig = JSON.parse(configJson);
    } catch {
      setError('Invalid JSON in Configuration field.');
      return;
    }

    const monitoredSymbols = symbols
      .split(',')
      .map((s) => s.trim().toUpperCase())
      .filter(Boolean);

    setSaving(true);
    try {
      if (isEditing && strategy) {
        await api.updateStrategy(strategy.id, {
          name,
          description,
          strategy_name: strategyName,
          status,
          mode,
          monitored_symbols: monitoredSymbols,
          cycle: Number(cycle),
          configuration: parsedConfig,
        });
      } else {
        await api.createStrategy({
          name,
          description,
          strategy_name: strategyName,
          status,
          mode,
          monitored_symbols: monitoredSymbols,
          cycle: Number(cycle),
          configuration: parsedConfig,
        });
      }
      onSaved();
    } catch (err: any) {
      setError(err.message || 'Failed to save strategy');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="modal-overlay">
      <div className="modal-content max-w-xl">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-bold text-green-500">
            {isEditing ? `Edit Strategy #${strategy?.id}` : 'Create New Strategy'}
          </h2>
          <button onClick={onClose} className="text-green-700 hover:text-green-400">
            <X className="w-5 h-5" />
          </button>
        </div>

        {error && (
          <div className="mb-4 p-3 bg-red-950/60 border border-red-800 text-red-300 rounded text-xs flex items-center gap-2">
            <AlertCircle className="w-4 h-4 flex-shrink-0" />
            <span>{error}</span>
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4 text-xs">
          <div className="grid grid-cols-2 gap-3">
            <div className="form-group mb-0">
              <label className="form-label">Strategy Name</label>
              <input
                type="text"
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. BTC Grid Scalper"
                className="form-input text-xs"
              />
            </div>

            <div className="form-group mb-0">
              <label className="form-label">Algorithm Key</label>
              <select
                value={strategyName}
                onChange={(e) => setStrategyName(e.target.value)}
                className="form-select text-xs"
              >
                <option value="grid">Grid (grid)</option>
                <option value="bollinger">Bollinger Bands (bollinger)</option>
                <option value="scalping">Scalping (scalping)</option>
                <option value="mlgrpc">ML gRPC (mlgrpc)</option>
              </select>
            </div>
          </div>

          <div className="form-group">
            <label className="form-label">Description</label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Strategy operational notes..."
              className="form-input text-xs"
            />
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="form-group mb-0">
              <label className="form-label">Status</label>
              <select
                value={status}
                onChange={(e) => setStatus(e.target.value as StrategyStatus)}
                className="form-select text-xs"
              >
                <option value="productive">Productive</option>
                <option value="testing">Testing</option>
                <option value="disabled">Disabled</option>
              </select>
            </div>

            <div className="form-group mb-0">
              <label className="form-label">Mode</label>
              <select
                value={mode}
                onChange={(e) => setMode(e.target.value as StrategyMode)}
                className="form-select text-xs"
              >
                <option value="dryrun">Dry Run</option>
                <option value="paper">Paper</option>
                <option value="live">LIVE</option>
                <option value="backtest">Backtest</option>
              </select>
            </div>

            <div className="form-group mb-0">
              <label className="form-label">Cycle (minutes)</label>
              <input
                type="number"
                min="1"
                required
                value={cycle}
                onChange={(e) => setCycle(Number(e.target.value))}
                className="form-input text-xs font-mono"
              />
            </div>
          </div>

          <div className="form-group">
            <label className="form-label">Monitored Symbols (comma separated)</label>
            <input
              type="text"
              required
              value={symbols}
              onChange={(e) => setSymbols(e.target.value)}
              placeholder="BTCUSDT, ETHUSDT"
              className="form-input text-xs font-mono"
            />
          </div>

          <div className="form-group">
            <label className="form-label">JSON Configuration</label>
            <textarea
              rows={6}
              value={configJson}
              onChange={(e) => setConfigJson(e.target.value)}
              className="form-textarea font-mono text-xs"
            />
          </div>

          <div className="flex justify-end gap-3 pt-2">
            <button type="button" onClick={onClose} className="btn btn-secondary">
              Cancel
            </button>
            <button type="submit" disabled={saving} className="btn btn-primary">
              {saving ? 'Saving...' : isEditing ? 'Update Strategy' : 'Create Strategy'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
