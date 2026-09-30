import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { Agent } from '@/api/types';
import { ApiError, apiErrorCode, apiErrorMessage } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { useAgents, useChatTranscript, useSendAgentMessage } from '@/hooks/queries';
import { useEditorBridge } from '@/context/EditorBridgeContext';
import { ChatContextKey, Turn, runToTurn, strategyContextKey, strategyIdOfKey } from '@/lib/chatTurn';
import { tr } from '@/i18n';

// One chat, two views (Phase D-02 §3). Agent mode (/agent) and the Code-mode
// dock both read this provider, so switching modes or routes never loses a
// transcript. Mounted once in App.tsx inside EditorBridgeProvider (it reads
// the Workbench's bridge for the Code-mode context) and inside the router
// (Agent mode's context comes from ?strategy=).

export const AGENT_MODE_PATH = '/agent';

/** /agent?strategy=<id>&agent=<id>, omitting whichever is unset. */
export function agentModeHref(strategyId?: number, agentId?: number): string {
  const qs = new URLSearchParams();
  if (strategyId != null) qs.set('strategy', String(strategyId));
  if (agentId != null) qs.set('agent', String(agentId));
  const q = qs.toString();
  return q ? `${AGENT_MODE_PATH}?${q}` : AGENT_MODE_PATH;
}

// Which persona answers, remembered in this browser (same key the old
// floating widget used). localStorage can throw - never fatal.
const AGENT_STORAGE_KEY = 'gtb_copilot_agent_id';

function loadStoredAgentId(): number | undefined {
  try {
    const raw = localStorage.getItem(AGENT_STORAGE_KEY);
    const n = raw ? Number(raw) : NaN;
    return Number.isFinite(n) && n > 0 ? n : undefined;
  } catch {
    return undefined;
  }
}

function storeAgentId(id: number | undefined) {
  try {
    if (id == null) localStorage.removeItem(AGENT_STORAGE_KEY);
    else localStorage.setItem(AGENT_STORAGE_KEY, String(id));
  } catch {
    /* storage unavailable - selection just won't persist */
  }
}

function positiveInt(raw: string | null): number | undefined {
  const n = raw ? Number(raw) : NaN;
  return Number.isInteger(n) && n > 0 ? n : undefined;
}

export interface ChatSession {
  isAgentMode: boolean;
  contextKey: ChatContextKey;
  /** The strategy the chat is about (undefined for general / new draft). */
  strategyId: number | undefined;
  agents: Agent[];
  selectedAgent: Agent | undefined;
  selectAgent: (id: number) => void;
  /** Turns loaded from the server (previous sessions), oldest first. */
  hydratedTurns: Turn[];
  /** Turns sent in this browser session, oldest first. */
  liveTurns: Turn[];
  /** True when hydrated turns are part of what the agent remembers (strategy
   * chats: the backend replays them). General chat only sends this session. */
  hydratedInModelContext: boolean;
  historyLoading: boolean;
  historyError: unknown;
  hasEarlier: boolean;
  loadingEarlier: boolean;
  loadEarlier: () => void;
  sending: boolean;
  send: (text: string) => void;
  /** The unsent composer text, shared by both views so it survives a mode switch. */
  draft: string;
  setDraft: (text: string) => void;
}

const ChatSessionContext = createContext<ChatSession | undefined>(undefined);

export function ChatSessionProvider({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const isAgentMode = location.pathname === AGENT_MODE_PATH;
  const search = useMemo(() => new URLSearchParams(location.search), [location.search]);
  const urlStrategyId = isAgentMode ? positiveInt(search.get('strategy')) : undefined;
  const urlAgentId = isAgentMode ? positiveInt(search.get('agent')) : undefined;

  const bridge = useEditorBridge();
  const bridgeRef = useRef(bridge);
  bridgeRef.current = bridge;

  // The "new strategy" draft becomes a real strategy mid-conversation when
  // the agent creates it (the run carries strategy_id): from then on the
  // chat is that strategy's DB-backed conversation. Reset when leaving the
  // Workbench so a later, unrelated draft starts clean (same rule as the old
  // floating widget).
  const [createdStrategyId, setCreatedStrategyId] = useState<number | undefined>(undefined);
  useEffect(() => {
    if (!bridge) setCreatedStrategyId(undefined);
  }, [bridge]);

  let contextKey: ChatContextKey;
  if (isAgentMode) {
    contextKey = urlStrategyId != null ? strategyContextKey(urlStrategyId) : 'general';
  } else if (bridge) {
    const sid = bridge.strategyId ?? createdStrategyId;
    contextKey = sid != null ? strategyContextKey(sid) : 'new';
  } else {
    contextKey = 'general';
  }
  const strategyId = strategyIdOfKey(contextKey);

  // --- persona ---------------------------------------------------------
  const { data: agents = [] } = useAgents();
  const [storedAgentId, setStoredAgentId] = useState<number | undefined>(() => loadStoredAgentId());
  const selectAgent = useCallback((id: number) => {
    setStoredAgentId(id);
    storeAgentId(id);
  }, []);
  // ?agent= wins when Agent mode opens (e.g. "Expand to Agent mode").
  useEffect(() => {
    if (urlAgentId != null) selectAgent(urlAgentId);
  }, [urlAgentId, selectAgent]);
  // A remembered id that no longer exists (agent deleted) falls back to the
  // default "Copilot" persona.
  const selectedAgent = agents.find((a) => a.id === storedAgentId) ?? agents.find((a) => a.is_default);

  // --- transcripts ------------------------------------------------------
  const [liveByKey, setLiveByKey] = useState<Partial<Record<ChatContextKey, Turn[]>>>({});
  const liveTurns = liveByKey[contextKey] ?? EMPTY;

  const transcript = useChatTranscript(contextKey);
  const hydratedTurns = useMemo(() => {
    const liveRunIds = new Set(liveTurns.map((t) => t.run?.id).filter((id): id is number => id != null));
    const runs = (transcript.data?.pages ?? []).flat().filter((r) => r && !liveRunIds.has(r.id));
    // Pages are newest first; the transcript reads oldest at the top.
    return runs.reverse().map(runToTurn);
  }, [transcript.data, liveTurns]);

  const updateTurn = useCallback((turnId: string, patch: Partial<Turn>) => {
    setLiveByKey((prev) => {
      const next: Partial<Record<ChatContextKey, Turn[]>> = {};
      for (const [k, turns] of Object.entries(prev) as [ChatContextKey, Turn[]][]) {
        next[k] = turns.some((t) => t.id === turnId) ? turns.map((t) => (t.id === turnId ? { ...t, ...patch } : t)) : turns;
      }
      return next;
    });
  }, []);

  const sendMessage = useSendAgentMessage();
  // Today's per-user spend lives on /auth/me (auth-02 §4) - re-read after
  // every turn so the budget line under the composer stays current.
  const { refresh: refreshMe } = useAuth();
  const sending = sendMessage.isPending;
  const [draft, setDraft] = useState('');

  const send = useCallback(
    (raw: string) => {
      const text = raw.trim();
      if (!text || sendMessage.isPending) return;
      const sendKey = contextKey;

      // Every prior turn of THIS session that actually completed - the
      // agent's memory of the conversation so far (strategy chats also get
      // their persisted history replayed server-side). Hydrated turns are
      // never re-sent; a failed/still-pending turn has nothing coherent to
      // replay.
      const history = (liveByKey[sendKey] ?? [])
        .filter((t) => t.run && t.run.status === 'ok')
        .map((t) => ({ input: t.input, tool_calls: t.run!.tool_calls, response_text: t.run!.response_text }));

      const turnId = `${Date.now()}-${Math.random()}`;
      const agentName = selectedAgent?.name;
      setLiveByKey((prev) => ({ ...prev, [sendKey]: [...(prev[sendKey] ?? []), { id: turnId, input: text, agentName }] }));

      sendMessage
        // Omitted agent_id = the backend's default agent (agents list not
        // loaded yet, or an older backend without personas).
        .mutateAsync({ input: text, strategyId: strategyIdOfKey(sendKey), history, agentId: selectedAgent?.id })
        .then((run) => {
          updateTurn(turnId, { run });
          // The draft just became a real strategy - move its transcript to
          // that strategy's conversation.
          if (sendKey === 'new' && run.strategy_id != null) {
            const newKey = strategyContextKey(run.strategy_id);
            setLiveByKey((prev) => ({ ...prev, new: [], [newKey]: [...(prev[newKey] ?? []), ...(prev.new ?? [])] }));
            if (bridgeRef.current && bridgeRef.current.strategyId == null) setCreatedStrategyId(run.strategy_id);
          }
        })
        .catch((err: unknown) => {
          // 409: a paused/halted agent, or (auth-01 §6) this user's daily
          // budget is used up - both render as the blocked-turn notice.
          // auth-01 contract (reconciled): 409 {"error":"user_budget_exceeded","message":...}
          if (err instanceof ApiError && err.status === 409) {
            updateTurn(turnId, {
              blocked: apiErrorMessage(err, tr('chat.agentPausedFallback')),
              blockedCode: apiErrorCode(err) ?? undefined,
            });
            return;
          }
          updateTurn(turnId, { failed: apiErrorMessage(err, err instanceof Error ? err.message : String(err)) });
        })
        .finally(() => {
          void refreshMe();
        });
    },
    [contextKey, liveByKey, selectedAgent, sendMessage, updateTurn, refreshMe],
  );

  const value = useMemo<ChatSession>(
    () => ({
      isAgentMode,
      contextKey,
      strategyId,
      agents,
      selectedAgent,
      selectAgent,
      hydratedTurns,
      liveTurns,
      hydratedInModelContext: strategyId != null,
      historyLoading: transcript.isLoading && contextKey !== 'new',
      historyError: transcript.error,
      hasEarlier: !!transcript.hasNextPage,
      loadingEarlier: transcript.isFetchingNextPage,
      loadEarlier: () => {
        if (transcript.hasNextPage && !transcript.isFetchingNextPage) transcript.fetchNextPage();
      },
      sending,
      send,
      draft,
      setDraft,
    }),
    [isAgentMode, contextKey, strategyId, agents, selectedAgent, selectAgent, hydratedTurns, liveTurns, transcript, sending, send, draft],
  );

  return <ChatSessionContext.Provider value={value}>{children}</ChatSessionContext.Provider>;
}

const EMPTY: Turn[] = [];

export function useChatSession(): ChatSession {
  const ctx = useContext(ChatSessionContext);
  if (!ctx) throw new Error('useChatSession must be used inside ChatSessionProvider');
  return ctx;
}
