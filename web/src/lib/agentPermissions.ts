import { AgentPermission } from '@/api/types';
import { tr, type MessageKey } from '@/i18n';

// The permission catalogue shown in the agent editor and as table badges.
// `phase` marks entries the API accepts but that grant nothing yet - they
// render disabled with that note. (None today: Phase B enabled
// create_strategy and propose_live.)
export const AGENT_PERMISSIONS: {
  key: AgentPermission;
  readonly label: string;
  readonly description: string;
  phase?: string;
}[] = (
  ['read', 'backtest', 'optimize', 'edit_testing', 'notify', 'create_strategy', 'propose_live', 'chain'] as AgentPermission[]
).map((key) => ({
  key,
  get label() {
    return tr(`agentPerms.${key}.label` as MessageKey);
  },
  get description() {
    return tr(`agentPerms.${key}.description` as MessageKey);
  },
}));

export function permissionLabel(key: string): string {
  return AGENT_PERMISSIONS.find((p) => p.key === key)?.label ?? key;
}
