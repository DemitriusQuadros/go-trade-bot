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
  Bot,
} from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { WALKTHROUGH_STORAGE_KEY } from '@/components/domain/Walkthrough';
import { useT } from '@/i18n';

// Section text lives in the i18n catalog (help.s.<key>.*). Inline markup in
// those strings: **bold** and `code` (see RichText below).
export interface HelpSection {
  slug: string;
  key: string;
  icon: React.ReactNode;
  // Paragraph keys (under help.s.<key>), then optional list-item keys.
  paragraphs: string[];
  list?: string[];
}

export const HELP_SECTIONS: HelpSection[] = [
  { slug: 'dashboard', key: 'dashboard', icon: <LayoutDashboard className="w-4 h-4 text-foreground" />, paragraphs: ['p1', 'p2'] },
  { slug: 'strategies', key: 'strategies', icon: <Layers className="w-4 h-4 text-success" />, paragraphs: ['p1', 'p2'] },
  {
    slug: 'strategy-builder',
    key: 'builder',
    icon: <Wand2 className="w-4 h-4 text-accent-foreground" />,
    paragraphs: ['p1'],
    list: ['li1', 'li2', 'li3', 'li4', 'li5'],
  },
  { slug: 'backtest', key: 'backtest', icon: <FlaskConical className="w-4 h-4 text-warning" />, paragraphs: ['p1', 'p2'] },
  { slug: 'optimization', key: 'optimization', icon: <Sliders className="w-4 h-4 text-primary" />, paragraphs: ['p1'] },
  { slug: 'activity', key: 'activity', icon: <DollarSign className="w-4 h-4 text-foreground" />, paragraphs: ['p1'] },
  { slug: 'agent-modes', key: 'agentModes', icon: <Bot className="w-4 h-4 text-foreground" />, paragraphs: ['p1', 'p2', 'p3'] },
  { slug: 'candle-import', key: 'candles', icon: <Download className="w-4 h-4 text-foreground" />, paragraphs: ['p1', 'p2'] },
  { slug: 'settings', key: 'settings', icon: <Settings className="w-4 h-4 text-muted-foreground" />, paragraphs: ['p1', 'p2'] },
];

// **bold** and `code` inside a translated string.
function RichText({ text }: { text: string }) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`)/g);
  return (
    <>
      {parts.map((part, i) => {
        if (part.startsWith('**') && part.endsWith('**')) {
          return (
            <strong key={i} className="text-foreground">
              {part.slice(2, -2)}
            </strong>
          );
        }
        if (part.startsWith('`') && part.endsWith('`')) {
          return (
            <span key={i} className="font-mono">
              {part.slice(1, -1)}
            </span>
          );
        }
        return <React.Fragment key={i}>{part}</React.Fragment>;
      })}
    </>
  );
}

function SectionBody({ section }: { section: HelpSection }) {
  const t = useT();
  const k = (leaf: string) => t.dyn(`help.s.${section.key}.${leaf}`);
  return (
    <div className="space-y-2 text-xs text-foreground leading-relaxed">
      {section.paragraphs.map((p) => (
        <p key={p}>
          <RichText text={k(p)} />
        </p>
      ))}
      {section.list && (
        <ul className="list-disc list-inside space-y-1 pl-2 text-foreground">
          {section.list.map((li) => (
            <li key={li}>
              <RichText text={k(li)} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function Help({ onReplayWalkthrough }: { onReplayWalkthrough?: () => void }) {
  const t = useT();
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
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <BookOpen className="w-6 h-6 text-foreground" /> {t('help.title')}
          </h1>
          <p className="text-xs text-muted-foreground mt-1">
            {t('help.subtitle')}
          </p>
        </div>

        <button
          type="button"
          onClick={handleReplay}
          className="px-3.5 py-2 rounded-lg bg-primary hover:bg-primary text-xs font-semibold text-white shadow flex items-center gap-2 self-start sm:self-auto transition-colors"
        >
          <PlayCircle className="w-4 h-4" />
          <span>{t('help.replay')}</span>
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
        {/* In-page navigation sidebar */}
        <div className="md:col-span-1 space-y-2">
          <div className="p-3 rounded-xl bg-card/80 border border-border space-y-1 sticky top-6">
            <h3 className="text-xs font-bold text-muted-foreground uppercase tracking-wider px-2 py-1">
              {t('help.toc')}
            </h3>
            <nav className="space-y-0.5">
              {HELP_SECTIONS.map((sec) => (
                <button
                  key={sec.slug}
                  type="button"
                  onClick={() => scrollToSection(sec.slug)}
                  className="w-full text-left px-2.5 py-1.5 rounded-lg text-xs font-medium text-foreground hover:text-white hover:bg-secondary flex items-center justify-between transition-colors group"
                >
                  <span className="flex items-center gap-2 truncate">
                    {sec.icon}
                    <span className="truncate">{t.dyn(`help.s.${sec.key}.title`)}</span>
                  </span>
                  <ChevronRight className="w-3 h-3 opacity-0 group-hover:opacity-100 text-muted-foreground shrink-0" />
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
                <div className="flex items-center gap-2.5 border-b border-border pb-3">
                  <div className="p-2 rounded-lg bg-background border border-border">
                    {sec.icon}
                  </div>
                  <div>
                    <h2 className="text-base font-bold text-foreground">{t.dyn(`help.s.${sec.key}.title`)}</h2>
                    <p className="text-xs text-muted-foreground">{t.dyn(`help.s.${sec.key}.summary`)}</p>
                  </div>
                </div>

                <div className="pt-1">
                  <SectionBody section={sec} />
                </div>
              </div>
            </Card>
          ))}
        </div>
      </div>
    </div>
  );
}
