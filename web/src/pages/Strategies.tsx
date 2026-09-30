import React, { useState, useMemo } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useStrategies } from '@/hooks/queries';
import { api, apiErrorMessage } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { canEditStrategy } from '@/lib/permissions';
import { Strategy, StrategyMode, StrategyStatus } from '@/api/types';
import { Card, CardHeader } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { LoadingScreen } from '@/components/ui/Spinner';
import { DropdownMenu, DropdownMenuItem } from '@/components/ui/DropdownMenu';
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
  GitBranch,
} from 'lucide-react';
import { useT } from '@/i18n';

export function Strategies() {
  const t = useT();
  const navigate = useNavigate();
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const { data: strategies = [], refetch: refetchStrategies, isLoading: isStrategiesLoading } = useStrategies();
  const [modeFilter, setModeFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');
  // Capability-aware (auth-02 §4): mode/status/enqueue are admin only;
  // friends create/edit/delete backtest drafts only.
  const { can } = useAuth();
  const isAdmin = can('admin');
  const canCreate = can('edit_drafts');

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
    confirmText: '',
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

  // Challenger links (B-02 §4), from the strategy DTO's challenger_of_id.
  const { strategyById, activeChallengersOf } = useMemo(() => {
    const byId = new Map<number, Strategy>();
    const challengers = new Map<number, Strategy[]>();
    (strategies ?? []).forEach((s) => byId.set(s.id, s));
    (strategies ?? []).forEach((s) => {
      if (s.challenger_of_id != null && s.status !== 'disabled') {
        const list = challengers.get(s.challenger_of_id) ?? [];
        list.push(s);
        challengers.set(s.challenger_of_id, list);
      }
    });
    return { strategyById: byId, activeChallengersOf: challengers };
  }, [strategies]);

  const handleToggleStatus = async (strat: Strategy) => {
    const newStatus: StrategyStatus = strat.status === 'disabled' ? 'productive' : 'disabled';
    try {
      await api.patchStrategyStatus(strat.id, newStatus);
      setActionMessage({ type: 'success', text: t('strategies.statusSet', { id: strat.id, status: t.enum('status', newStatus) }) });
      refetchStrategies();
    } catch (err: any) {
      setActionMessage({ type: 'error', text: apiErrorMessage(err, t('strategies.statusFailed')) });
    }
  };

  const handleModeChange = (strat: Strategy, newMode: StrategyMode) => {
    if (newMode === 'live') {
      setConfirmDialog({
        isOpen: true,
        title: t('strategies.liveTitle'),
        message: t('strategies.liveMessage', { name: strat.name }),
        confirmText: t('strategies.liveConfirm'),
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
      setActionMessage({ type: 'success', text: t('strategies.modeSwitched', { id, mode: t.enum('modeOption', mode) }) });
      refetchStrategies();
    } catch (err: any) {
      setActionMessage({ type: 'error', text: apiErrorMessage(err, t('strategies.modeFailed')) });
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
      title: t('strategies.deleteTitle'),
      message: t('strategies.deleteMessage', { name: strat.name, id: strat.id }),
      confirmText: t('strategies.deleteConfirm'),
      isDangerous: true,
      onConfirm: async () => {
        setConfirmDialog((prev) => ({ ...prev, isOpen: false }));
        try {
          await api.deleteStrategy(strat.id);
          setActionMessage({ type: 'success', text: t('strategies.deleted', { id: strat.id }) });
          refetchStrategies();
        } catch (err: any) {
          setActionMessage({ type: 'error', text: apiErrorMessage(err, t('strategies.deleteFailed')) });
        }
      },
    });
  };

  const handleEnqueue = async () => {
    try {
      await api.enqueueStrategy();
      setActionMessage({ type: 'success', text: t('strategies.enqueued') });
    } catch (err: any) {
      setActionMessage({ type: 'error', text: apiErrorMessage(err, t('strategies.enqueueFailed')) });
    }
  };

  if (isStrategiesLoading && !strategies) {
    return <LoadingScreen message={t('strategies.loading')} />;
  }

  return (
    <div className="container-custom space-y-6">
      {/* Header */}
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            {t('strategies.title')}
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('strategies.subtitle')}
          </p>
        </div>

        <div className="flex items-center gap-3">
          {isAdmin && (
          <button
            onClick={handleEnqueue}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs flex items-center gap-1.5 px-3 py-1.5"
            title={t('strategies.enqueueTickTitle')}
          >
            <Send className="w-3.5 h-3.5 text-primary" />
            <span>{t('strategies.enqueueTick')}</span>
          </button>
          )}

          {canCreate && (
          <button
            data-walkthrough="new-strategy-btn"
            onClick={() => navigate('/strategies/new')}
            className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs flex items-center gap-1.5 px-3 py-1.5 font-bold"
          >
            <Plus className="w-4 h-4" />
            <span>{t('strategies.newStrategy')}</span>
          </button>
          )}
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
              placeholder={t('strategies.searchPlaceholder')}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="form-input text-xs"
            />
          </div>

          <div className="flex items-center gap-3 flex-wrap">
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground font-medium">{t('strategies.statusLabel')}</span>
              <select
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value)}
                className="form-select text-xs py-1"
              >
                <option value="all">{t('strategies.allStatuses')}</option>
                <option value="productive">{t.enum('statusOption', 'productive')}</option>
                <option value="testing">{t.enum('statusOption', 'testing')}</option>
                <option value="disabled">{t.enum('statusOption', 'disabled')}</option>
              </select>
            </div>

            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground font-medium">{t('strategies.modeLabel')}</span>
              <select
                value={modeFilter}
                onChange={(e) => setModeFilter(e.target.value)}
                className="form-select text-xs py-1"
              >
                <option value="all">{t('strategies.allModes')}</option>
                <option value="dryrun">{t.enum('modeOption', 'dryrun')}</option>
                <option value="paper">{t.enum('modeOption', 'paper')}</option>
                <option value="live">{t.enum('modeOption', 'live')}</option>
                <option value="backtest">{t.enum('modeOption', 'backtest')}</option>
              </select>
            </div>

            <button
              onClick={() => refetchStrategies()}
              className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-xs p-1.5"
              title={t('common.refresh')}
            >
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </Card>

      {/* Strategies List Table */}
      <Card>
        <CardHeader
          title={t('strategies.listTitle')}
          subtitle={t('strategies.listSubtitle', { shown: filteredStrategies.length, total: strategies?.length || 0 })}
        />

        {strategies.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg space-y-3">
            <p>{t('strategies.empty')}</p>
            {canCreate && (
            <button
              onClick={() => navigate('/strategies/new')}
              className="bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary text-xs px-3 py-1.5 font-bold"
            >
              {t('strategies.newStrategyPlus')}
            </button>
            )}
          </div>
        ) : filteredStrategies.length === 0 ? (
          <div className="p-8 text-center text-xs text-muted-foreground bg-secondary/40 rounded-lg">
            {t('strategies.noMatch')}
          </div>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full border-collapse">
              <thead>
                <tr className="border-b border-border bg-secondary/40">
                  <th className="px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('strategies.colStrategy')}</th>
                  <th className="px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('strategies.colSymbols')}</th>
                  <th className="px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('strategies.colStatus')}</th>
                  <th className="px-4 py-2.5 text-left text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('strategies.colMode')}</th>
                  <th className="w-10 px-2 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {filteredStrategies.map((strat) => {
                  const editable = canEditStrategy(can, strat);
                  const items: DropdownMenuItem[] = [
                    {
                      label: editable ? t('strategies.viewEditScript') : t('strategies.viewScript'),
                      icon: <Code2 />,
                      onClick: () => navigate(`/strategies/${strat.id}/edit`),
                    },
                    {
                      label: t('strategies.agentHistory'),
                      icon: <Bot />,
                      onClick: () => navigate(`/activity?tab=agent&strategy_id=${strat.id}`),
                    },
                  ];
                  if (isAdmin) {
                    items.push({
                      label: strat.status === 'disabled' ? t('strategies.enable') : t('strategies.disable'),
                      icon: strat.status === 'disabled' ? <Play /> : <Pause />,
                      onClick: () => handleToggleStatus(strat),
                    });
                  }
                  if (editable) {
                    items.push({
                      label: t('common.delete'),
                      icon: <Trash2 />,
                      onClick: () => handleDelete(strat),
                      destructive: true,
                      separatorBefore: true,
                    });
                  }
                  return (
                  <tr
                    key={strat.id}
                    onClick={() => navigate(`/strategies/${strat.id}/edit`)}
                    className="border-b border-border last:border-b-0 hover:bg-accent/40 cursor-pointer transition-colors"
                  >
                    <td className="px-4 py-3 align-top max-w-xs">
                      <div className="font-semibold text-foreground truncate">{strat.name}</div>
                      {strat.challenger_of_id != null && (
                        <div className="text-[11px] text-muted-foreground mt-0.5 flex items-center gap-1 min-w-0">
                          <GitBranch className="w-3 h-3 shrink-0" />
                          <span className="shrink-0">{t('strategies.challengerOf')}</span>
                          <Link
                            to={`/strategies/${strat.challenger_of_id}/edit`}
                            onClick={(e) => e.stopPropagation()}
                            className="text-foreground hover:text-primary hover:underline truncate"
                          >
                            {strategyById.get(strat.challenger_of_id)?.name ?? `#${strat.challenger_of_id}`}
                          </Link>
                        </div>
                      )}
                      {(activeChallengersOf.get(strat.id)?.length ?? 0) > 0 && (
                        <div className="mt-1 flex flex-wrap gap-1">
                          {(() => {
                            const list = activeChallengersOf.get(strat.id)!;
                            return (
                              <Link
                                to={`/strategies/${list[0].id}/edit`}
                                onClick={(e) => e.stopPropagation()}
                                title={list.map((c) => `#${c.id} ${c.name}`).join('\n')}
                                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-primary/15 text-primary border border-primary/40 text-[10px] font-semibold hover:bg-primary/25"
                              >
                                <GitBranch className="w-3 h-3" />
                                {t('strategies.challengers', { count: list.length })}
                              </Link>
                            );
                          })()}
                        </div>
                      )}
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
                        disabled={!isAdmin}
                        title={isAdmin ? undefined : t('users.adminOnly')}
                        aria-label={t('strategies.modeOf', { name: strat.name })}
                        className="bg-secondary border border-border text-xs rounded px-2 py-1 text-foreground focus:outline-none focus:border-primary font-medium disabled:opacity-60 disabled:cursor-not-allowed"
                      >
                        <option value="dryrun">{t.enum('modeOption', 'dryrun')}</option>
                        <option value="paper">{t.enum('modeOption', 'paper')}</option>
                        <option value="live">{t.enum('mode', 'live')}</option>
                        <option value="backtest">{t.enum('modeOption', 'backtest')}</option>
                      </select>
                    </td>
                    <td className="px-2 py-3 align-top text-right" onClick={(e) => e.stopPropagation()}>
                      <DropdownMenu label={t('strategies.actionsFor', { name: strat.name })} items={items} />
                    </td>
                  </tr>
                  );
                })}
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
