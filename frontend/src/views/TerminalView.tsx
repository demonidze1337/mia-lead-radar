import { useEffect, useMemo, useRef, useState } from 'react';
import { Pause, Play, Radio, RefreshCw } from 'lucide-react';
import { api, connectTerminal, type PipelineStats, type TerminalEvent } from '../api/client';
import { Badge, EmptyState, Panel, SectionTitle, Toggle } from '../components/ui';
import { useI18n } from '../i18n';

const LEVELS = ['all', 'info', 'warn', 'error', 'success'] as const;

export default function TerminalView() {
  const { t } = useI18n();
  const [events, setEvents] = useState<TerminalEvent[]>([]);
  const [level, setLevel] = useState<(typeof LEVELS)[number]>('all');
  const [autoScroll, setAutoScroll] = useState(true);
  const [status, setStatus] = useState<'open' | 'closed' | 'error'>('closed');
  const [stats, setStats] = useState<PipelineStats | null>(null);
  const [paused, setPaused] = useState(false);
  const [busy, setBusy] = useState(false);
  const bottom = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const disconnect = connectTerminal({
      onEvent: (event) => setEvents((current) => [...current.slice(-599), event]),
      onStatus: setStatus,
    });
    return disconnect;
  }, []);

  useEffect(() => {
    let cancelled = false;
    const load = () => api.pipelineStats().then((value) => { if (!cancelled) { setStats(value); setPaused(value.paused); } }).catch(() => undefined);
    void load();
    const timer = setInterval(load, 5000);
    return () => { cancelled = true; clearInterval(timer); };
  }, []);

  const filtered = useMemo(() => (level === 'all' ? events : events.filter((event) => event.level === level)), [events, level]);

  useEffect(() => {
    if (autoScroll) bottom.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [filtered.length, autoScroll]);

  const togglePipeline = async () => {
    setBusy(true);
    try {
      if (paused) await api.resumePipeline();
      else await api.pausePipeline();
      setPaused(!paused);
      setStats(await api.pipelineStats());
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <SectionTitle
        eyebrow="WebSocket"
        title={t('terminal')}
        subtitle={t('terminalSubtitle')}
        actions={(
          <>
            <button className="btn" onClick={() => void togglePipeline()} disabled={busy}>
              {paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />} {paused ? t('resume') : t('pause')}
            </button>
            <button className="btn h-9 w-9 p-0" onClick={() => setEvents([])} title={t('refresh')}><RefreshCw className="h-4 w-4" /></button>
          </>
        )}
      />

      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Badge tone={status === 'open' ? 'mint' : status === 'error' ? 'rose' : 'amber'}>{status}</Badge>
        {stats && (
          <>
            <span className="pill">{stats.processed_msgs} {t('messages').toLowerCase()}</span>
            <span className="pill">{stats.qualified_leads} {t('leads').toLowerCase()}</span>
            <span className="pill">{stats.active_accounts} active · {stats.flooded_accounts} flood</span>
          </>
        )}
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <select className="field max-w-[9rem]" value={level} onChange={(event) => setLevel(event.target.value as (typeof LEVELS)[number])}>
            {LEVELS.map((item) => <option key={item} value={item}>{item === 'all' ? t('all') : item}</option>)}
          </select>
          <label className="flex items-center gap-2 text-xs text-slate-400"><Toggle checked={autoScroll} onChange={setAutoScroll} /> {t('autoscroll')}</label>
        </div>
      </div>

      <Panel className="overflow-hidden">
        <div className="flex items-center gap-2 border-b border-white/[0.06] px-4 py-2 text-xs text-slate-500">
          <Radio className={`h-3.5 w-3.5 ${status === 'open' ? 'animate-pulse text-mint' : 'text-slate-600'}`} />
          Live event stream
          <span className="ml-auto">{filtered.length} events</span>
        </div>
        <div className="h-[60vh] overflow-y-auto p-3 font-mono text-[11px] leading-relaxed">
          {filtered.length === 0 ? <EmptyState text={t('empty')} /> : filtered.map((event) => (
            <div key={`${event.id}-${event.timestamp}`} className="mb-1 flex gap-2 rounded px-2 py-1 hover:bg-white/[0.02]">
              <span className="shrink-0 text-slate-600">{new Date(event.timestamp).toLocaleTimeString()}</span>
              <span className={`shrink-0 ${event.level === 'error' ? 'text-rose-300' : event.level === 'warn' ? 'text-amber-200' : event.level === 'success' ? 'text-mint' : 'text-cyan'}`}>[{event.type}]</span>
              <span className="min-w-0 break-words text-slate-300">{event.message}</span>
              {event.data && <span className="hidden shrink-0 text-slate-600 sm:inline">{JSON.stringify(event.data)}</span>}
            </div>
          ))}
          <div ref={bottom} />
        </div>
      </Panel>
    </div>
  );
}
