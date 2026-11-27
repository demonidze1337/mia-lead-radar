import { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, BellOff, BellRing, Gauge, ShieldCheck } from 'lucide-react';
import {
  api, type DailyMetric, type FunnelSummary, type RadarProfile, type TagStat, type TrashSample,
} from '../api/client';
import { Badge, EmptyState, ErrorState, Panel, SectionTitle, Skeleton, Stat } from '../components/ui';
import { useI18n } from '../i18n';

export default function AnalyticsView() {
  const { t } = useI18n();
  const [radars, setRadars] = useState<RadarProfile[]>([]);
  const [radarID, setRadarID] = useState(0);
  const [days, setDays] = useState(7);
  const [funnel, setFunnel] = useState<FunnelSummary | null>(null);
  const [daily, setDaily] = useState<DailyMetric[]>([]);
  const [trash, setTrash] = useState<TrashSample[]>([]);
  const [tags, setTags] = useState<TagStat[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [noiseMessage, setNoiseMessage] = useState('');

  const load = async (selected = radarID) => {
    setLoading(true);
    setError('');
    try {
      const radarList = await api.listRadars();
      setRadars(radarList);
      const chosen = selected || (radarList.find((item) => item.is_active) ?? radarList[0])?.id || 0;
      setRadarID(chosen);
      const [summary, metrics, samples, tagStats] = await Promise.all([
        api.funnel(chosen || undefined, days),
        api.daily(chosen || undefined, days),
        api.trashSamples(chosen || undefined),
        api.tagStats(chosen || undefined),
      ]);
      setFunnel(summary);
      setDaily(metrics);
      setTrash(samples);
      setTags(tagStats);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const maximumDaily = useMemo(() => Math.max(...daily.map((item) => item.messages_seen), 1), [daily]);

  const checkNoise = async () => {
    if (!radarID) return;
    setBusy(true);
    setNoiseMessage('');
    try {
      const result = await api.checkNoiseGuard(radarID);
      setNoiseMessage(result.alert ? `${result.alert.recommended_action} ${result.alert.throttled_tags.length ? `(${result.alert.throttled_tags.join(', ')})` : ''}` : 'No noisy tags above threshold.');
      await load(radarID);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const toggleThrottle = async (tag: TagStat) => {
    setBusy(true);
    try {
      const active = tag.throttled_until && new Date(tag.throttled_until).getTime() > Date.now();
      await api.throttleTag(radarID, tag.tag, undefined, Boolean(active));
      await load(radarID);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  if (loading && !funnel) return <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} />)}</div>;
  if (error && !funnel) return <ErrorState error={error} onRetry={() => void load()} />;

  return (
    <div>
      <SectionTitle
        eyebrow="Noise Guard"
        title={t('analytics')}
        subtitle={t('analyticsSubtitle')}
        actions={(
          <>
            <select className="field max-w-[14rem]" value={radarID} onChange={(event) => { const value = Number(event.target.value); setRadarID(value); void load(value); }}>
              <option value={0}>{t('all')}</option>
              {radars.map((radar) => <option key={radar.id} value={radar.id}>{radar.name}</option>)}
            </select>
            <select className="field max-w-[7rem]" value={days} onChange={(event) => { const value = Number(event.target.value); setDays(value); void load(radarID); }}>
              {[1, 7, 14, 30, 90].map((value) => <option key={value} value={value}>{value}d</option>)}
            </select>
            <button className="btn" disabled={busy || !radarID} onClick={() => void checkNoise()}><ShieldCheck className="h-4 w-4" /> {t('noiseGuard')}</button>
          </>
        )}
      />
      {error && <p className="mb-3 rounded-xl border border-rose-400/20 bg-rose-400/10 p-3 text-sm text-rose-200">{error}</p>}
      {noiseMessage && <p className="mb-3 rounded-xl border border-amber-300/20 bg-amber-300/[0.07] p-3 text-sm text-amber-100">{noiseMessage}</p>}

      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat label={t('messages')} value={funnel?.messages_seen ?? 0} />
        <Stat label={t('passed')} value={funnel?.tier0_passed ?? 0} tone="cyan" />
        <Stat label={t('leads')} value={funnel?.total_leads ?? 0} tone="mint" hint={`${funnel?.clear_count ?? 0} clear`} />
        <Stat label={t('trashRatio')} value={`${(funnel?.trash_ratio_pct ?? 0).toFixed(1)}%`} tone={funnel?.noise_guard_triggered ? 'rose' : 'amber'} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.2fr_0.8fr]">
        <Panel className="p-5">
          <div className="mb-4 flex items-center justify-between"><h3 className="flex items-center gap-2 text-sm font-semibold"><Gauge className="h-4 w-4 text-cyan" /> {t('funnel')}</h3><Badge tone={funnel?.noise_guard_triggered ? 'rose' : 'mint'}>{funnel?.noise_guard_triggered ? t('noiseGuard') : 'Clean'}</Badge></div>
          <div className="space-y-3">
            {[
              { label: t('messages'), value: funnel?.messages_seen ?? 0, color: 'bg-cyan/50' },
              { label: t('passed'), value: funnel?.tier0_passed ?? 0, color: 'bg-mint/50' },
              { label: t('leads'), value: funnel?.total_leads ?? 0, color: 'bg-emerald-300/50' },
              { label: t('viewed'), value: funnel?.viewed_count ?? 0, color: 'bg-amber-300/50' },
              { label: t('profileOpened'), value: funnel?.profile_opened_count ?? 0, color: 'bg-violet-300/50' },
            ].map((item) => (
              <div key={item.label}>
                <div className="mb-1 flex justify-between text-xs"><span className="text-slate-400">{item.label}</span><span className="text-slate-200">{item.value}</span></div>
                <div className="h-2 overflow-hidden rounded-full bg-white/[0.04]"><div className={`h-full ${item.color}`} style={{ width: `${Math.max(2, (item.value / Math.max(funnel?.messages_seen ?? 1, 1)) * 100)}%` }} /></div>
              </div>
            ))}
          </div>
        </Panel>

        <Panel className="p-5">
          <h3 className="mb-3 text-sm font-semibold">{t('daily')}</h3>
          <div className="flex h-40 items-end gap-1">
            {daily.length === 0 ? <EmptyState text={t('noData')} /> : daily.map((metric) => (
              <div key={metric.date} className="group flex flex-1 flex-col items-center justify-end gap-1">
                <span className="hidden text-[9px] text-slate-400 group-hover:block">{metric.messages_seen}</span>
                <div className="w-full rounded-t bg-cyan/40 transition-colors duration-150 group-hover:bg-cyan/70" style={{ height: `${Math.max(3, (metric.messages_seen / maximumDaily) * 100)}%` }} title={`${metric.date}: ${metric.messages_seen}`} />
              </div>
            ))}
          </div>
          <div className="mt-2 flex justify-between text-[10px] text-slate-500"><span>{daily[0]?.date ?? ''}</span><span>{daily[daily.length - 1]?.date ?? ''}</span></div>
        </Panel>
      </div>

      <div className="mt-4 grid gap-4 xl:grid-cols-2">
        <Panel className="p-5">
          <div className="mb-3 flex items-center justify-between"><h3 className="text-sm font-semibold">{t('noisyTags')}</h3><Badge tone="rose">{tags.filter(isThrottled).length} {t('throttle').toLowerCase()}</Badge></div>
          {tags.length === 0 ? <EmptyState text={t('noData')} /> : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead className="text-slate-500"><tr><th className="py-1">Tag</th><th>Hits</th><th>Trash</th><th>FP</th><th /></tr></thead>
                <tbody>
                  {tags.slice(0, 20).map((stat) => {
                    const active = isThrottled(stat);
                    return (
                      <tr key={`${stat.radar_id}-${stat.tag}`} className="border-t border-white/[0.04]">
                        <td className="max-w-[12rem] truncate py-1.5 text-slate-200">{stat.tag}</td>
                        <td className="text-slate-400">{stat.total_hits}</td>
                        <td className={stat.trash_hits > 0 ? 'text-rose-300' : 'text-slate-500'}>{stat.trash_hits}</td>
                        <td className="text-amber-200">{stat.false_positive_hits}</td>
                        <td className="text-right"><button className={`btn h-7 px-2 text-[10px] ${active ? 'border-amber-300/30 text-amber-200' : ''}`} disabled={busy} onClick={() => void toggleThrottle(stat)}>{active ? <><BellOff className="h-3 w-3" /> {t('unthrottle')}</> : <><BellRing className="h-3 w-3" /> {t('throttle')}</>}</button></td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Panel>

        <Panel className="p-5">
          <h3 className="mb-3 text-sm font-semibold">{t('trashSamples')}</h3>
          {trash.length === 0 ? <EmptyState text={t('noData')} /> : (
            <div className="max-h-[42vh] space-y-2 overflow-y-auto pr-1">
              {trash.map((sample) => (
                <div key={sample.id} className="rounded-xl border border-white/[0.05] bg-white/[0.02] p-2.5">
                  <div className="flex flex-wrap items-center gap-2 text-[10px]"><Badge tone="rose">{sample.source}</Badge><span className="text-slate-500">{sample.reason} · {sample.lang} · {sample.score}</span></div>
                  <p className="mt-1 line-clamp-2 text-xs text-slate-400">{sample.text_excerpt}</p>
                </div>
              ))}
            </div>
          )}
        </Panel>
      </div>

      {funnel?.noise_guard_triggered && (
        <div className="mt-4 flex items-center gap-2 rounded-xl border border-rose-400/20 bg-rose-400/[0.06] p-3 text-sm text-rose-200">
          <AlertTriangle className="h-4 w-4" /> {funnel.trash_ratio_pct.toFixed(1)}% trash. Throttle broad tags or tighten stop phrases.
        </div>
      )}
    </div>
  );
}

function isThrottled(stat: TagStat): boolean {
  return Boolean(stat.throttled_until && new Date(stat.throttled_until).getTime() > Date.now());
}
