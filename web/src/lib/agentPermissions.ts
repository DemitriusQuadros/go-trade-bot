import { AgentPermission } from '@/api/types';

// The permission catalogue shown in the agent editor and as table badges.
// `phase` marks entries the API accepts but that grant nothing yet - they
// render disabled with that note. (None today: Phase B enabled
// create_strategy and propose_live.)
export const AGENT_PERMISSIONS: {
  key: AgentPermission;
  label: string;
  description: string;
  phase?: string;
}[] = [
  { key: 'read', label: 'Read', description: 'Inspect strategies, backtests, positions and performance.' },
  { key: 'backtest', label: 'Backtest', description: 'Run backtests.' },
  { key: 'optimize', label: 'Optimize', description: 'Run parameter optimizations.' },
  {
    key: 'edit_testing',
    label: 'Edit testing',
    description: 'edit and auto-deploy non-live strategies, only if the deploy gate passes',
  },
  { key: 'notify', label: 'Notify', description: 'Send webhook notifications to the selected targets.' },
  {
    key: 'create_strategy',
    label: 'Create strategy',
    description: 'create new strategies (always testing/dryrun, max 3/day)',
  },
  {
    key: 'propose_live',
    label: 'Propose live',
    description: 'propose promotions of challenger code into live strategies (you approve every one)',
  },
];

export function permissionLabel(key: string): string {
  return AGENT_PERMISSIONS.find((p) => p.key === key)?.label ?? key;
}
