import { useEffect, useState } from 'react';
import { ArrowRight, CircleAlert, Gauge, Radar, Radio, Sparkles } from 'lucide-react';
import { api, type FunnelSummary, type Lead, type PipelineStats } from '../api/client';
import { Panel, SectionTitle, Stat, Skeletons, ErrorState, Badge } from '../components/ui';
import { useApp } from '../appContext';
import { useI18n } from '../i18n';

export default function DashboardView() {
  const { t } = useI18n();
  const { navigate, settings } = useApp();
  const [funnel, setFunnel] = useState<FunnelSummary | null>(null);
  const [stats, setStats] = useState<PipelineStats | null>(null);
  const [leads, setLeads] = useState<Lead[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const [summary, pipeline, feed] = await Promise.all([
        api.funnel(undefined, 7),
        api.pipelineStats(),
        api.listLeads({ limit: 5 }),
      ]);
      setFunnel(summary);
      setStats(pipeline);
      setLeads(feed.leads);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  if (loading) return <Skeletons rows={4} />;
  if (error) return <ErrorState error={error} onRetry={() => void load()} />;

  return (
    <div>
      <SectionTitle
        eyebrow="MIA"
        title={t('dashboard')}
        subtitle={t('dashboardSubtitle')}
        actions={<button className="btn" onClick={() => void load()}><Gauge className="h-4 w-4" /> {t('refresh')}</button>}
      />

      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat label={t('messages')} value={funnel?.messages_seen ?? 0} hint={`${funnel?.tier0_rejected ?? 0} ${t('rejected').toLowerCase()}`} />
        <Stat label={t('leads')} value={funnel?.total_leads ?? 0} tone="cyan" hint={`${funnel?.clear_count ?? 0} clear`} />
        <Stat label={t('viewed')} value={funnel?.viewed_count ?? 0} tone="amber" hint={`${funnel?.profile_opened_count ?? 0} ${t('profileOpened').toLowerCase()}`} />
        <Stat label={t('savedTime')} value={`${Math.round((funnel?.saved_time_seconds ?? 0) / 60)}m`} tone="mint" hint={`${settings.min_lead_score}+ score`} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.5fr_1fr]">
        <Panel className="p-5">
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h3 className="text-sm font-semibold">{t('funnel')}</h3>
              <p className="text-xs text-slate-500">{funnel?.days ?? 7} days</p>
            </div>
            <Badge tone={funnel?.noise_guard_triggered ? 'rose' : 'mint'}>
              {funnel?.noise_guard_triggered ? t('noiseGuard') : 'Healthy'}
            </Badge>
          </div>
          <FunnelBars funnel={funnel} />
        </Panel>

        <Panel className="p-5">
          <h3 className="mb-4 text-sm font-semibold">{t('terminal')}</h3>
          <div className="space-y-2 text-sm">
            <Row label={t('status')} value={stats?.running ? (stats.paused ? t('pause') : 'Running') : 'Stopped'} />
            <Row label={t('accounts')} value={`${stats?.active_accounts ?? 0} active · ${stats?.flooded_accounts ?? 0} flood`} />
            <Row label={t('messages')} value={String(stats?.processed_msgs ?? 0)} />
            <Row label={t('leads')} value={String(stats?.qualified_leads ?? 0)} />
          </div>
          {stats?.last_error && <p className="mt-3 rounded-lg border border-rose-400/20 bg-rose-400/10 p-2 text-xs text-rose-200">{stats.last_error}</p>}
          <button className="btn mt-4 w-full" onClick={() => navigate('terminal')}><Radio className="h-4 w-4" /> {t('terminal')} <ArrowRight className="ml-auto h-4 w-4" /></button>
        </Panel>
      </div>

      <Panel className="mt-4 p-5">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-semibold">{t('leads')}</h3>
          <button className="btn" onClick={() => navigate('leads')}>{t('all')} <ArrowRight className="h-4 w-4" /></button>
        </div>
        {leads.length === 0 ? <p className="py-8 text-center text-sm text-slate-500">{t('empty')}</p> : (
          <div className="space-y-2">
            {leads.map((lead) => (
              <button key={lead.id} onClick={() => navigate('leads')} className="flex w-full items-start gap-3 rounded-xl border border-white/[0.05] bg-white/[0.02] p-3 text-left hover:border-mint/25">
                <span className={`mt-1 h-2 w-2 shrink-0 rounded-full ${lead.category === 'clear' ? 'bg-mint' : lead.category === 'potential' ? 'bg-cyan' : 'bg-amber-300'}`} />
                <span className="min-w-0 flex-1">
                  <span className="flex flex-wrap items-center gap-2 text-xs text-slate-400">
                    <b className="text-slate-100">{lead.score}</b> · {lead.chat_title || 'Telegram'} · {lead.lang.toUpperCase()}
                    {lead.status === 'new' && <Badge tone="mint">{t('new')}</Badge>}
                  </span>
                  <span className="mt-1 line-clamp-2 block text-sm text-slate-300">{lead.text}</span>
                </span>
              </button>
            ))}
          </div>
        )}
      </Panel>

      {funnel?.noise_guard_triggered && (
        <div className="mt-4 flex items-start gap-3 rounded-2xl border border-amber-300/25 bg-amber-300/[0.07] p-4 text-sm text-amber-100">
          <CircleAlert className="mt-0.5 h-5 w-5 shrink-0" />
          <div>
            <p className="font-medium">{t('noiseGuard')}</p>
            <p className="text-xs text-amber-200/80">{funnel.trash_ratio_pct.toFixed(1)}% {t('trashRatio').toLowerCase()} · {funnel.noisy_tags.length} noisy tags. Review Analytics and throttle broad phrases.</p>
          </div>
          <button className="btn ml-auto border-amber-300/30 text-amber-100" onClick={() => navigate('analytics')}>{t('analytics')}</button>
        </div>
      )}

      <div className="mt-4 grid gap-3 sm:grid-cols-3">
        <QuickAction icon={Radar} title={t('radar')} subtitle={t('radarSubtitle')} onClick={() => navigate('radar')} />
        <QuickAction icon={Sparkles} title={t('consultant')} subtitle={t('consultantSubtitle')} onClick={() => navigate('consultant')} />
        <QuickAction icon={Gauge} title={t('analytics')} subtitle={t('analyticsSubtitle')} onClick={() => navigate('analytics')} />
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return <div className="flex items-center justify-between gap-3 border-b border-white/[0.04] pb-2 last:border-0"><span className="text-slate-500">{label}</span><span className="text-right text-slate-200">{value}</span></div>;
}

function FunnelBars({ funnel }: { funnel: FunnelSummary | null }) {
  const { t } = useI18n();
  const items = [
    { label: t('messages'), value: funnel?.messages_seen ?? 0, color: 'bg-cyan/60' },
    { label: t('passed'), value: funnel?.tier0_passed ?? 0, color: 'bg-mint/60' },
    { label: t('leads'), value: funnel?.total_leads ?? 0, color: 'bg-emerald-400/60' },
    { label: t('viewed'), value: funnel?.viewed_count ?? 0, color: 'bg-amber-300/60' },
    { label: t('profileOpened'), value: funnel?.profile_opened_count ?? 0, color: 'bg-violet-300/60' },
  ];
  const max = Math.max(...items.map((item) => item.value), 1);
  return (
    <div className="space-y-3">
      {items.map((item) => (
        <div key={item.label}>
          <div className="mb-1 flex items-center justify-between text-xs"><span className="text-slate-400">{item.label}</span><span className="tabular-nums text-slate-200">{item.value}</span></div>
          <div className="h-2 overflow-hidden rounded-full bg-white/[0.04]"><div className={`h-full rounded-full ${item.color}`} style={{ width: `${Math.max(2, (item.value / max) * 100)}%` }} /></div>
        </div>
      ))}
      <div className="flex items-center justify-between pt-2 text-xs text-slate-500">
        <span>{t('trashRatio')}</span>
        <span className={funnel?.noise_guard_triggered ? 'text-rose-300' : 'text-mint'}>{(funnel?.trash_ratio_pct ?? 0).toFixed(1)}%</span>
      </div>
    </div>
  );
}

function QuickAction({ icon: Icon, title, subtitle, onClick }: { icon: typeof Radar; title: string; subtitle: string; onClick: () => void }) {
  return (
    <button onClick={onClick} className="panel group flex items-start gap-3 p-4 text-left transition-colors duration-150 hover:border-mint/30">
      <span className="flex h-9 w-9 items-center justify-center rounded-xl border border-white/10 bg-white/[0.03] text-mint"><Icon className="h-4 w-4" /></span>
      <span><span className="block text-sm font-medium">{title}</span><span className="line-clamp-2 text-xs text-slate-500">{subtitle}</span></span>
      <ArrowRight className="ml-auto h-4 w-4 text-slate-600 group-hover:text-mint" />
    </button>
  );
}

