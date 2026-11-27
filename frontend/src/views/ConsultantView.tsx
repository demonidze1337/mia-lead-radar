import { useEffect, useMemo, useState } from 'react';
import { BrainCircuit, Check, MessageCircle, Sparkles, Wand2 } from 'lucide-react';
import {
  api, type AuditResult, type Lead, type RadarProfile, type RoleplayReply, type RoleplayReview,
} from '../api/client';
import { Badge, EmptyState, Panel, SectionTitle, SpinnerButton } from '../components/ui';
import { useI18n } from '../i18n';

export default function ConsultantView() {
  const { t } = useI18n();
  const [tab, setTab] = useState<'audit' | 'roleplay'>('audit');
  const [radars, setRadars] = useState<RadarProfile[]>([]);
  const [leads, setLeads] = useState<Lead[]>([]);
  const [radarID, setRadarID] = useState(0);
  const [leadID, setLeadID] = useState(0);
  const [question, setQuestion] = useState('');
  const [audit, setAudit] = useState<AuditResult | null>(null);
  const [auditBusy, setAuditBusy] = useState(false);
  const [patchBusy, setPatchBusy] = useState(false);
  const [patchApplied, setPatchApplied] = useState(false);
  const [sellerReply, setSellerReply] = useState('');
  const [dialog, setDialog] = useState<RoleplayReply[]>([]);
  const [roleplayBusy, setRoleplayBusy] = useState(false);
  const [review, setReview] = useState<RoleplayReview | null>(null);
  const [reviewBusy, setReviewBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    Promise.all([api.listRadars(), api.listLeads({ limit: 100 })])
      .then(([radarList, leadList]) => {
        setRadars(radarList);
        setLeads(leadList.leads);
        const radar = radarList.find((item) => item.is_active) ?? radarList[0];
        if (radar) setRadarID(radar.id);
        let stored = 0;
        try { stored = Number(sessionStorage.getItem('mia_roleplay_lead') ?? 0); } catch { /* ignore */ }
        const lead = leadList.leads.find((item) => item.id === stored) ?? leadList.leads[0];
        if (lead) setLeadID(lead.id);
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  const selectedLead = useMemo(() => leads.find((lead) => lead.id === leadID), [leads, leadID]);

  const runAudit = async () => {
    if (!radarID) return;
    setAuditBusy(true);
    setError('');
    setPatchApplied(false);
    try {
      setAudit(await api.audit(radarID, question));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setAuditBusy(false);
    }
  };

  const applyPatch = async () => {
    if (!audit?.apply_radar_settings) return;
    setPatchBusy(true);
    setError('');
    try {
      await api.applyPatch(radarID, audit.apply_radar_settings);
      setPatchApplied(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPatchBusy(false);
    }
  };

  const sendRoleplay = async () => {
    if (!selectedLead || !sellerReply.trim()) return;
    const text = sellerReply.trim();
    setRoleplayBusy(true);
    setError('');
    try {
      const reply = await api.roleplay(selectedLead.radar_id, selectedLead.id, text);
      setDialog((current) => [...current, reply]);
      setSellerReply('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRoleplayBusy(false);
    }
  };

  const coach = async () => {
    if (!selectedLead) return;
    setReviewBusy(true);
    setError('');
    try {
      setReview(await api.reviewRoleplay(selectedLead.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setReviewBusy(false);
    }
  };

  return (
    <div>
      <SectionTitle eyebrow="AI" title={t('consultant')} subtitle={t('consultantSubtitle')} />

      <div className="mb-4 flex gap-2">
        <button className={`btn ${tab === 'audit' ? 'border-mint/40 bg-mint/10 text-mint' : ''}`} onClick={() => setTab('audit')}><BrainCircuit className="h-4 w-4" /> {t('audit')}</button>
        <button className={`btn ${tab === 'roleplay' ? 'border-mint/40 bg-mint/10 text-mint' : ''}`} onClick={() => setTab('roleplay')}><MessageCircle className="h-4 w-4" /> Roleplay</button>
      </div>

      {error && <p className="mb-3 rounded-xl border border-rose-400/20 bg-rose-400/10 p-3 text-sm text-rose-200">{error}</p>}

      {tab === 'audit' ? (
        <div className="grid gap-4 xl:grid-cols-[0.8fr_1.2fr]">
          <Panel className="p-5">
            <span className="label">Radar</span>
            <select className="field mb-3" value={radarID} onChange={(event) => setRadarID(Number(event.target.value))}>
              {radars.map((radar) => <option key={radar.id} value={radar.id}>{radar.name} (v{radar.version})</option>)}
            </select>
            <label><span className="label">{t('question')}</span><textarea className="field min-h-28" value={question} onChange={(event) => setQuestion(event.target.value)} placeholder="Audit my radar noise, tags and funnel conversion..." /></label>
            <SpinnerButton loading={auditBusy} className="btn-primary mt-3 w-full" onClick={() => void runAudit()}><Wand2 className="h-4 w-4" /> {t('audit')}</SpinnerButton>
          </Panel>

          <Panel className="p-5">
            {!audit ? <EmptyState text={t('empty')} /> : (
              <div>
                <div className="mb-3 rounded-xl border border-mint/20 bg-mint/[0.05] p-3 text-sm text-slate-200">{audit.reply}</div>
                <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500">{t('diagnosis')}</h3>
                <p className="mb-3 mt-1 text-sm text-slate-300">{audit.diagnosis}</p>
                <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500">{t('recommendations')}</h3>
                <ul className="mb-4 mt-1 space-y-1 text-sm text-slate-300">
                  {audit.recommendations.map((item) => <li key={item} className="flex gap-2"><Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-mint" />{item}</li>)}
                </ul>
                {audit.apply_radar_settings && (
                  <div className="rounded-xl border border-cyan/20 bg-cyan/[0.05] p-3">
                    <div className="mb-2 flex items-center justify-between"><Badge tone="cyan">APPLY_RADAR_SETTINGS</Badge><span className="text-xs text-slate-400">{audit.apply_radar_settings.summary}</span></div>
                    <p className="mb-2 text-xs text-slate-400">{audit.apply_radar_settings.description}</p>
                    <button className="btn-primary w-full" disabled={patchBusy || patchApplied} onClick={() => void applyPatch()}>
                      {patchApplied ? <><Check className="h-4 w-4" /> {t('saved')}</> : <><Sparkles className="h-4 w-4" /> {t('apply')}</>}
                    </button>
                  </div>
                )}
              </div>
            )}
          </Panel>
        </div>
      ) : (
        <div className="grid gap-4 xl:grid-cols-[0.7fr_1.3fr]">
          <Panel className="p-5">
            <span className="label">Lead</span>
            <select className="field mb-3" value={leadID} onChange={(event) => setLeadID(Number(event.target.value))}>
              {leads.map((lead) => <option key={lead.id} value={lead.id}>#{lead.id} · {lead.chat_title} · {lead.score}</option>)}
            </select>
            {selectedLead && (
              <div className="rounded-xl border border-white/[0.06] bg-white/[0.02] p-3 text-sm text-slate-300">
                <p className="mb-1 text-[11px] text-slate-500">{selectedLead.chat_title} · {selectedLead.lang}</p>
                {selectedLead.text}
              </div>
            )}
            <div className="mt-4 flex gap-2">
              <button className="btn w-full" onClick={() => void coach()} disabled={reviewBusy || !selectedLead}>{t('coachReview')}</button>
              <button className="btn" onClick={() => { setDialog([]); setReview(null); }}>{t('refresh')}</button>
            </div>
            {review && (
              <div className="mt-4 rounded-xl border border-violet-400/20 bg-violet-400/[0.06] p-3 text-xs">
                <div className="mb-2 flex items-center justify-between"><Badge tone="violet">{review.score}/100</Badge></div>
                <p className="text-mint">+ {review.strengths.join(' · ')}</p>
                <p className="text-amber-200">− {review.weaknesses.join(' · ')}</p>
                <p className="mt-2 text-slate-300">{review.improved_text}</p>
              </div>
            )}
          </Panel>

          <Panel className="flex min-h-[60vh] flex-col p-4">
            <div className="flex-1 space-y-3 overflow-y-auto">
              {dialog.length === 0 ? <EmptyState text={t('empty')} /> : dialog.map((reply, index) => (
                <div key={index} className="ml-auto max-w-[85%] rounded-2xl rounded-br-sm border border-mint/20 bg-mint/[0.08] p-3 text-sm text-slate-200">
                  {reply.message}
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-[10px] text-slate-500"><Badge tone="mint">{reply.mood}</Badge><span>{reply.hidden_note}</span></div>
                </div>
              ))}
            </div>
            <div className="mt-3 flex gap-2 border-t border-white/[0.06] pt-3">
              <input className="field" value={sellerReply} onChange={(event) => setSellerReply(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void sendRoleplay(); }} placeholder="Your seller reply..." />
              <SpinnerButton loading={roleplayBusy} className="btn-primary" onClick={() => void sendRoleplay()}>{t('send')}</SpinnerButton>
            </div>
          </Panel>
        </div>
      )}
    </div>
  );
}
