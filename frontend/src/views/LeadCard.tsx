import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import {
  Check, ChevronDown, Copy, ExternalLink, Flag, MessageCircle, Sparkles, UserRound,
} from 'lucide-react';
import { api, type Lead, type LeadStatus } from '../api/client';
import { Badge } from '../components/ui';
import { useI18n } from '../i18n';
import { useApp } from '../appContext';
import {
  buildMessageLink, buildProfileLink, copyAuthorIdentifier, openTelegramTarget,
} from '../utils/telegramLinks';
import { playLeadChime, triggerHaptic } from '../utils/feedback';

const CATEGORY_TONE = {
  clear: 'mint',
  potential: 'cyan',
  doubtful: 'amber',
  trash: 'rose',
} as const;

const STATUS_OPTIONS: LeadStatus[] = ['new', 'in_progress', 'replied', 'closed', 'false_positive', 'hidden'];

export default function LeadCard({ lead, onChanged }: { lead: Lead; onChanged: () => void }) {
  const { t } = useI18n();
  const { settings, navigate } = useApp();
  const [expanded, setExpanded] = useState(false);
  const [icebreakers, setIcebreakers] = useState<string[]>(lead.icebreakers);
  const [icebreakerStyle, setIcebreakerStyle] = useState('consultative');
  const [busy, setBusy] = useState(false);
  const [learnStops, setLearnStops] = useState(true);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState('');
  const [viewed, setViewed] = useState(Boolean(lead.viewed_at));

  const profileLink = buildProfileLink(lead);
  const messageLink = buildMessageLink(lead);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1600);
    return () => clearTimeout(timer);
  }, [copied]);

  const expand = async () => {
    const next = !expanded;
    setExpanded(next);
    if (next && !viewed) {
      setViewed(true);
      try {
        await api.leadViewed(lead.id);
        onChanged();
      } catch {
        setViewed(false);
      }
    }
  };

  const openProfile = async () => {
    if (!profileLink) return;
    try { await api.leadProfileOpened(lead.id); } catch { /* telemetry is best-effort */ }
    openTelegramTarget(profileLink);
  };

  const copyIdentifier = async () => {
    const value = await copyAuthorIdentifier(lead);
    if (value) {
      setCopied(true);
      triggerHaptic(settings.haptic_enabled);
    }
  };

  const generateIcebreaker = async () => {
    setBusy(true);
    setError('');
    try {
      const result = await api.leadIcebreaker(lead.id, icebreakerStyle);
      setIcebreakers(result.drafts);
      playLeadChime(settings.sound_enabled);
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const changeStatus = async (status: LeadStatus) => {
    setBusy(true);
    try {
      await api.leadStatus(lead.id, status);
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const markFalsePositive = async () => {
    setBusy(true);
    setError('');
    try {
      await api.leadFalsePositive(lead.id, learnStops);
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const practice = () => {
    try { sessionStorage.setItem('mia_roleplay_lead', String(lead.id)); } catch { /* ignore */ }
    navigate('consultant');
  };

  return (
    <motion.article layout initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} className="panel overflow-hidden">
      <button className="w-full p-4 text-left" onClick={() => void expand()}>
        <div className="flex items-start gap-3">
          <div className={`mt-1 flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border text-xs font-bold ${
            lead.score >= 85 ? 'border-mint/30 bg-mint/10 text-mint' : lead.score >= 61 ? 'border-cyan/30 bg-cyan/10 text-cyan' : 'border-amber-300/25 bg-amber-300/10 text-amber-200'
          }`}>{lead.score}</div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-slate-500">
              {!viewed && <Badge tone="mint">{t('new')}</Badge>}
              <Badge tone={CATEGORY_TONE[lead.category] ?? 'slate'}>{lead.category}</Badge>
              <span>{lead.chat_title || 'Telegram'}</span>
              <span>·</span>
              <span>{lead.lang.toUpperCase()}</span>
              {lead.matched_tags?.slice(0, 3).map((tag) => <span key={tag} className="rounded bg-white/[0.04] px-1.5 py-0.5 text-[10px] text-slate-400">{tag}</span>)}
            </div>
            <p className={`mt-2 text-sm leading-relaxed text-slate-200 ${expanded ? '' : 'line-clamp-2'}`}>{lead.text}</p>
            <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px] text-slate-500">
              <span>{lead.author_name || 'Unknown'}</span>
              {lead.author_username && <span>@{lead.author_username}</span>}
              {lead.reason && <span className="truncate">· {lead.reason}</span>}
            </div>
          </div>
          <ChevronDown className={`h-4 w-4 shrink-0 text-slate-500 transition-transform duration-150 ${expanded ? 'rotate-180' : ''}`} />
        </div>
      </button>

      {expanded && (
        <div className="border-t border-white/[0.06] px-4 pb-4 pt-3">
          {error && <p className="mb-2 text-xs text-rose-300">{error}</p>}
          <div className="mb-3 flex flex-wrap gap-2">
            <button className="btn" disabled={!profileLink} onClick={() => void openProfile()}><UserRound className="h-4 w-4" /> {t('openProfile')}</button>
            <button className="btn" disabled={!messageLink} onClick={() => openTelegramTarget(messageLink)}><ExternalLink className="h-4 w-4" /> {t('openMessage')}</button>
            <button className="btn" onClick={() => void copyIdentifier()}><Copy className="h-4 w-4" /> {copied ? t('copied') : t('copyId')}</button>
            {lead.msg_link && <a className="btn" href={lead.msg_link} target="_blank" rel="noreferrer"><MessageCircle className="h-4 w-4" /> t.me</a>}
          </div>

          <div className="mb-3 flex flex-wrap items-center gap-2">
            <select className="field max-w-[11rem]" value={lead.status} onChange={(event) => void changeStatus(event.target.value as LeadStatus)} disabled={busy}>
              {STATUS_OPTIONS.map((status) => <option key={status} value={status}>{status.replace('_', ' ')}</option>)}
            </select>
            <label className="flex items-center gap-2 text-xs text-slate-400">
              <input type="checkbox" checked={learnStops} onChange={(event) => setLearnStops(event.target.checked)} />
              {t('learnedStopWords')}
            </label>
            <button className="btn-danger" disabled={busy} onClick={() => void markFalsePositive()}><Flag className="h-4 w-4" /> {t('falsePositive')}</button>
            <button className="btn" onClick={practice}><Sparkles className="h-4 w-4" /> {t('practice')}</button>
          </div>

          <div className="rounded-xl border border-white/[0.06] bg-white/[0.02] p-3">
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <span className="text-xs font-medium text-slate-300">{t('icebreaker')}</span>
              <select className="rounded-lg border border-white/10 bg-ink/80 px-2 py-1 text-xs text-slate-300" value={icebreakerStyle} onChange={(event) => setIcebreakerStyle(event.target.value)}>
                <option value="consultative">consultative</option>
                <option value="direct">direct</option>
                <option value="question">question</option>
              </select>
              <button className="btn ml-auto h-8" disabled={busy} onClick={() => void generateIcebreaker()}>
                {busy ? <span className="h-3 w-3 animate-pulse rounded-full bg-mint" /> : <Sparkles className="h-3.5 w-3.5" />} {t('regenerate')}
              </button>
            </div>
            {icebreakers.length === 0 ? <p className="text-xs text-slate-500">{t('noData')}</p> : (
              <div className="space-y-2">
                {icebreakers.map((draft, index) => (
                  <div key={`${index}-${draft.slice(0, 12)}`} className="group flex items-start gap-2 rounded-lg bg-white/[0.03] p-2">
                    <p className="flex-1 text-xs leading-relaxed text-slate-300">{draft}</p>
                    <button className="btn h-7 w-7 p-0 opacity-70" onClick={() => void navigator.clipboard?.writeText(draft)} title={t('copyId')}>
                      <Copy className="h-3 w-3" />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
          {viewed && <p className="mt-2 flex items-center gap-1 text-[10px] text-mint/70"><Check className="h-3 w-3" /> {t('viewed')}</p>}
        </div>
      )}
    </motion.article>
  );
}

