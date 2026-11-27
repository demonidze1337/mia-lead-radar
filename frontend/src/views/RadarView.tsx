import { useEffect, useMemo, useState } from 'react';
import { Beaker, BrainCircuit, Plus, Save, Sparkles, Trash2, Wand2 } from 'lucide-react';
import { api, type EvalResult, type RadarProfile, type RadarTag, type RadarVersion, type SimulateResult } from '../api/client';
import { Badge, EmptyState, ErrorState, Loading, Panel, SectionTitle, SpinnerButton, Toggle } from '../components/ui';
import { useI18n, LANGUAGES } from '../i18n';
import { useApp } from '../appContext';

const EMPTY_RADAR: Partial<RadarProfile> = {
  name: 'My radar',
  description: '',
  target_languages: ['en', 'ru'],
  tags: [],
  stop_phrases: [],
  learned_stop_words: [],
  allow_hr_vacancies: false,
  allow_partnerships: false,
  quality_score: 0,
  quality_notes: '',
  is_active: true,
};

export default function RadarView() {
  const { t, language } = useI18n();
  const { settings, refreshSetup } = useApp();
  const [radars, setRadars] = useState<RadarProfile[]>([]);
  const [draft, setDraft] = useState<Partial<RadarProfile>>(EMPTY_RADAR);
  const [versions, setVersions] = useState<RadarVersion[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [simMessage, setSimMessage] = useState('');
  const [useLLM, setUseLLM] = useState(true);
  const [simResult, setSimResult] = useState<SimulateResult | null>(null);
  const [simBusy, setSimBusy] = useState(false);
  const [newTag, setNewTag] = useState('');
  const [newTagKind, setNewTagKind] = useState<'root' | 'live'>('root');
  const [stopText, setStopText] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const list = await api.listRadars();
      setRadars(list);
      const chosen = list.find((radar) => radar.is_active) ?? list[0];
      if (chosen) {
        setDraft(chosen);
        setStopText(chosen.stop_phrases.join('\n'));
        void loadVersions(chosen.id);
      } else {
        setDraft(EMPTY_RADAR);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const loadVersions = async (id: number) => {
    try { setVersions(await api.radarVersions(id)); } catch { setVersions([]); }
  };

  useEffect(() => { void load(); }, []);

  const tags = useMemo(() => draft.tags ?? [], [draft.tags]);
  const selectedLanguages = draft.target_languages ?? [];

  const toggleLanguage = (code: string) => {
    setDraft((current) => {
      const list = current.target_languages ?? [];
      return { ...current, target_languages: list.includes(code) ? list.filter((item) => item !== code) : [...list, code] };
    });
  };

  const updateTag = (index: number, patch: Partial<RadarTag>) => {
    setDraft((current) => ({ ...current, tags: (current.tags ?? []).map((tag, i) => (i === index ? { ...tag, ...patch } : tag)) }));
  };

  const addTag = () => {
    const phrase = newTag.trim();
    if (!phrase) return;
    setDraft((current) => ({ ...current, tags: [...(current.tags ?? []), { phrase, kind: newTagKind, weight: 1, enabled: true }] }));
    setNewTag('');
  };

  const save = async () => {
    setBusy(true);
    setError('');
    try {
      const payload: Partial<RadarProfile> = {
        ...draft,
        stop_phrases: stopText.split('\n').map((line) => line.trim()).filter(Boolean),
      };
      const saved = draft.id ? await api.updateRadar(payload as RadarProfile) : await api.createRadar(payload);
      setDraft(saved);
      setStopText(saved.stop_phrases.join('\n'));
      await load();
      await refreshSetup();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const suggest = async () => {
    if (!draft.description?.trim()) {
      setError('Add a business description first');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const result = await api.suggestRadar(draft.description, selectedLanguages.length ? selectedLanguages : settings.target_languages, language);
      const suggested: RadarTag[] = result.tags.map((tag) => ({
        phrase: tag.phrase,
        kind: tag.kind === 'live' ? 'live' : 'root',
        weight: tag.weight || 1,
        enabled: true,
      }));
      setDraft((current) => ({
        ...current,
        tags: [...(current.tags ?? []), ...suggested],
        stop_phrases: [...new Set([...(current.stop_phrases ?? []), ...result.stop_phrases])],
        quality_score: result.quality_score,
        quality_notes: result.quality_notes,
      }));
      setStopText((current) => [...new Set([...current.split('\n').filter(Boolean), ...result.stop_phrases])].join('\n'));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const simulate = async () => {
    if (!simMessage.trim()) return;
    if (!draft.id) {
      setError('Save the radar before running a dry-run');
      return;
    }
    setSimBusy(true);
    setError('');
    try {
      setSimResult(await api.simulate({
        radar_id: draft.id,
        message: simMessage,
        use_llm: useLLM,
        author_id: 1,
        author_name: 'Simulator',
        chat_id: 1,
        msg_id: 1,
      }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSimBusy(false);
    }
  };

  if (loading) return <Loading />;
  if (error && radars.length === 0) return <ErrorState error={error} onRetry={() => void load()} />;

  return (
    <div>
      <SectionTitle
        eyebrow="Tier-0"
        title={t('radar')}
        subtitle={t('radarSubtitle')}
        actions={(
          <>
            <button className="btn" onClick={() => void suggest()} disabled={busy}><Sparkles className="h-4 w-4" /> {t('suggest')}</button>
            <SpinnerButton loading={busy} onClick={() => void save()}><Save className="h-4 w-4" /> {t('save')}</SpinnerButton>
          </>
        )}
      />

      {error && <p className="mb-3 rounded-xl border border-rose-400/20 bg-rose-400/10 p-3 text-sm text-rose-200">{error}</p>}

      <div className="grid gap-4 xl:grid-cols-[1.3fr_0.7fr]">
        <Panel className="p-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <label><span className="label">Name</span><input className="field" value={draft.name ?? ''} onChange={(event) => setDraft({ ...draft, name: event.target.value })} /></label>
            <div className="flex items-end gap-3">
              <div><span className="label">{t('score')}</span><Badge tone={(draft.quality_score ?? 0) >= 70 ? 'mint' : 'amber'}>{draft.quality_score ?? 0}/100</Badge></div>
              <div className="ml-auto flex items-center gap-2 text-sm text-slate-300">
                <Toggle checked={Boolean(draft.is_active)} onChange={(value) => setDraft({ ...draft, is_active: value })} />
                Active
              </div>
            </div>
          </div>
          <label className="mt-4 block"><span className="label">Business offer / description</span>
            <textarea className="field min-h-24 resize-y" value={draft.description ?? ''} onChange={(event) => setDraft({ ...draft, description: event.target.value })} placeholder="I build high-converting websites and landing pages for SaaS teams..." />
          </label>

          <div className="mt-4">
            <span className="label">{t('language')}</span>
            <div className="flex flex-wrap gap-1.5">
              {LANGUAGES.map((item) => (
                <button key={item.code} type="button" onClick={() => toggleLanguage(item.code)} className={`rounded-full border px-2.5 py-1 text-[11px] ${selectedLanguages.includes(item.code) ? 'border-mint/40 bg-mint/10 text-mint' : 'border-white/10 bg-white/[0.03] text-slate-400'}`}>
                  {item.label}
                </button>
              ))}
            </div>
          </div>

          <div className="mt-5">
            <div className="mb-2 flex items-center justify-between"><span className="label mb-0">{t('tags')} ({tags.length})</span><span className="text-[11px] text-slate-500">70% root · 30% live</span></div>
            <div className="space-y-2">
              {tags.map((tag, index) => (
                <div key={`${tag.phrase}-${index}`} className="flex items-center gap-2 rounded-xl border border-white/[0.05] bg-white/[0.02] p-2">
                  <input className="field flex-1 border-0 bg-transparent px-1" value={tag.phrase} onChange={(event) => updateTag(index, { phrase: event.target.value })} />
                  <select className="rounded-lg border border-white/10 bg-ink/80 px-2 py-1 text-xs text-slate-300" value={tag.kind} onChange={(event) => updateTag(index, { kind: event.target.value as 'root' | 'live' })}>
                    <option value="root">{t('root')}</option><option value="live">{t('live')}</option>
                  </select>
                  <input className="w-16 rounded-lg border border-white/10 bg-ink/80 px-2 py-1 text-xs text-slate-300" type="number" min="0.5" max="2" step="0.1" value={tag.weight} onChange={(event) => updateTag(index, { weight: Number(event.target.value) })} />
                  <Toggle checked={tag.enabled} onChange={(value) => updateTag(index, { enabled: value })} />
                  <button className="btn h-7 w-7 p-0 text-slate-500" onClick={() => setDraft({ ...draft, tags: tags.filter((_, i) => i !== index) })}><Trash2 className="h-3.5 w-3.5" /></button>
                </div>
              ))}
            </div>
            <div className="mt-2 flex gap-2">
              <input className="field flex-1" value={newTag} onChange={(event) => setNewTag(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') addTag(); }} placeholder="website development" />
              <select className="field max-w-[6.5rem]" value={newTagKind} onChange={(event) => setNewTagKind(event.target.value as 'root' | 'live')}><option value="root">root</option><option value="live">live</option></select>
              <button className="btn" onClick={addTag}><Plus className="h-4 w-4" /></button>
            </div>
          </div>

          <div className="mt-5 grid gap-4 sm:grid-cols-2">
            <label><span className="label">{t('stopPhrases')}</span><textarea className="field min-h-24" value={stopText} onChange={(event) => setStopText(event.target.value)} placeholder={'giveaway\nказино'} /></label>
            <div>
              <span className="label">{t('learnedStopWords')}</span>
              <div className="flex min-h-24 flex-wrap content-start gap-1.5 rounded-xl border border-white/10 bg-ink/50 p-2">
                {(draft.learned_stop_words ?? []).length === 0 ? <span className="text-xs text-slate-500">{t('noData')}</span> : (draft.learned_stop_words ?? []).map((word) => <Badge key={word} tone="violet">{word}</Badge>)}
              </div>
            </div>
          </div>

          <div className="mt-4 flex flex-wrap gap-4">
            <label className="flex items-center gap-2 text-sm text-slate-300"><Toggle checked={Boolean(draft.allow_hr_vacancies)} onChange={(value) => setDraft({ ...draft, allow_hr_vacancies: value })} /> {t('allowHr')}</label>
            <label className="flex items-center gap-2 text-sm text-slate-300"><Toggle checked={Boolean(draft.allow_partnerships)} onChange={(value) => setDraft({ ...draft, allow_partnerships: value })} /> {t('allowPartnerships')}</label>
          </div>
        </Panel>

        <div className="space-y-4">
          <Panel className="p-5">
            <div className="mb-3 flex items-center gap-2"><Beaker className="h-4 w-4 text-cyan" /><h3 className="text-sm font-semibold">{t('simulator')}</h3></div>
            <textarea className="field min-h-24" value={simMessage} onChange={(event) => setSimMessage(event.target.value)} placeholder="I need a developer to build a website for my business, budget 3000..." />
            <div className="mt-3 flex items-center justify-between gap-2">
              <label className="flex items-center gap-2 text-xs text-slate-400"><Toggle checked={useLLM} onChange={setUseLLM} /> Fast LLM</label>
              <SpinnerButton loading={simBusy} onClick={() => void simulate()} className="btn-primary"><BrainCircuit className="h-4 w-4" /> {t('runTest')}</SpinnerButton>
            </div>
            {simResult && <SimulationResult result={simResult} />}
          </Panel>

          <Panel className="p-5">
            <h3 className="mb-3 flex items-center gap-2 text-sm font-semibold"><Wand2 className="h-4 w-4 text-violet-300" /> {t('versions')}</h3>
            {versions.length === 0 ? <EmptyState text={t('noData')} /> : (
              <div className="space-y-2">
                {versions.map((version) => (
                  <div key={version.id} className="rounded-xl border border-white/[0.05] bg-white/[0.02] p-2 text-xs">
                    <div className="flex items-center justify-between"><Badge tone="cyan">v{version.version}</Badge><span className="text-slate-500">{version.change_source}</span></div>
                    <p className="mt-1 text-slate-400">{version.audit_summary || version.description || t('noData')}</p>
                  </div>
                ))}
              </div>
            )}
          </Panel>
        </div>
      </div>
    </div>
  );
}

function SimulationResult({ result }: { result: SimulateResult }) {
  const { t } = useI18n();
  const evaluation: EvalResult = result.eval;
  return (
    <div className="mt-4 rounded-xl border border-white/[0.07] bg-ink/40 p-3 text-xs">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={evaluation.passed ? 'mint' : 'rose'}>{evaluation.passed ? t('passed') : t('rejected')}</Badge>
        {!evaluation.passed && <span className="text-rose-300">{evaluation.reject_reason}</span>}
        <span className="text-slate-400">{evaluation.detected_lang.toUpperCase()} · {(evaluation.latency_micros / 1000).toFixed(2)} ms</span>
        <span className="ml-auto text-slate-300">score {evaluation.score.toFixed(1)}</span>
      </div>
      <div className="mt-2 grid grid-cols-2 gap-1 text-[11px] text-slate-400">
        <span>BM25 {evaluation.breakdown.bm25_score.toFixed(1)}</span>
        <span>Intent {evaluation.breakdown.intent_score.toFixed(1)}</span>
        <span>DIY {evaluation.breakdown.diy_fatigue_score.toFixed(1)}</span>
        <span>Seller {evaluation.breakdown.seller_penalty.toFixed(1)}</span>
      </div>
      {evaluation.matched_tags?.length ? <p className="mt-2 text-[11px] text-slate-400">matched: {evaluation.matched_tags.join(', ')}</p> : null}
      {result.qualify && (
        <div className="mt-2 border-t border-white/[0.06] pt-2 text-slate-300">
          LLM: {result.qualify.score} · {result.qualify.category} · {result.qualify.reason}
        </div>
      )}
      {result.llm_error && <p className="mt-2 text-amber-200">{result.llm_error}</p>}
    </div>
  );
}
