import { useState } from 'react';
import { Bell, Bot, BrainCircuit, KeyRound, Save, Shield, SlidersHorizontal } from 'lucide-react';
import { api, type LLMConfig, type RuntimeSettings } from '../api/client';
import { Badge, Panel, SectionTitle, SpinnerButton, Toggle } from '../components/ui';
import { useI18n, LANGUAGES, type Language } from '../i18n';
import { useApp } from '../appContext';

export default function SettingsView() {
  const { t, language, setLanguage } = useI18n();
  const { settings, refreshSettings, openSetup } = useApp();
  const [draft, setDraft] = useState<RuntimeSettings>(settings);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [models, setModels] = useState<string[]>([]);

  const updateLLM = (patch: Partial<LLMConfig>) => setDraft({ ...draft, llm: { ...draft.llm, ...patch } });

  const save = async () => {
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const saved = await api.saveSettings(draft);
      setDraft(saved);
      setLanguage(saved.ui_language as Language);
      await refreshSettings();
      setMessage(t('saved'));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const testLLM = async () => {
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const result = await api.testLLM(draft.llm);
      setModels(result.models ?? []);
      setMessage(result.ok ? `Connected · ${(result.models ?? []).length} models` : result.error ?? 'Connection failed');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const saveBot = async () => {
    setBusy(true);
    setError('');
    try {
      await api.saveBot(draft.owner_bot_token ?? '', draft.owner_telegram_id);
      setMessage(t('saved'));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <SectionTitle
        eyebrow="Runtime"
        title={t('settings')}
        subtitle={t('settingsSubtitle')}
        actions={(
          <>
            <button className="btn" onClick={openSetup}>{t('openSetup')}</button>
            <SpinnerButton loading={busy} onClick={() => void save()}><Save className="h-4 w-4" /> {t('save')}</SpinnerButton>
          </>
        )}
      />
      {(message || error) && <p className={`mb-3 rounded-xl border p-3 text-sm ${error ? 'border-rose-400/20 bg-rose-400/10 text-rose-200' : 'border-mint/20 bg-mint/[0.06] text-mint'}`}>{error || message}</p>}

      <div className="grid gap-4 xl:grid-cols-2">
        <Panel className="p-5">
          <h3 className="mb-4 flex items-center gap-2 text-sm font-semibold"><SlidersHorizontal className="h-4 w-4 text-cyan" /> General</h3>
          <label><span className="label">{t('uiLanguage')}</span>
            <select className="field" value={draft.ui_language} onChange={(event) => { setDraft({ ...draft, ui_language: event.target.value }); setLanguage(event.target.value as Language); }}>
              {LANGUAGES.map((item) => <option key={item.code} value={item.code}>{item.label}</option>)}
            </select>
          </label>
          <div className="mt-4 grid gap-3 sm:grid-cols-2">
            <NumberField label={t('score') + ' min'} value={draft.min_lead_score} onChange={(value) => setDraft({ ...draft, min_lead_score: value })} />
            <NumberField label={t('noiseThreshold')} value={draft.noise_trash_threshold_pct} step={1} onChange={(value) => setDraft({ ...draft, noise_trash_threshold_pct: value })} />
          </div>
          <div className="mt-4 space-y-3">
            <ToggleRow label={t('sound')} checked={draft.sound_enabled} onChange={(value) => setDraft({ ...draft, sound_enabled: value })} />
            <ToggleRow label={t('haptics')} checked={draft.haptic_enabled} onChange={(value) => setDraft({ ...draft, haptic_enabled: value })} />
            <ToggleRow label={t('noiseGuard')} checked={draft.noise_guard_enabled} onChange={(value) => setDraft({ ...draft, noise_guard_enabled: value })} />
            <ToggleRow label="Auto read history" checked={draft.auto_read_enabled} onChange={(value) => setDraft({ ...draft, auto_read_enabled: value })} />
            <ToggleRow label="Auto join" checked={draft.auto_join_enabled} onChange={(value) => setDraft({ ...draft, auto_join_enabled: value })} />
          </div>
          <label className="mt-4 block"><span className="label">{t('webhookUrl')}</span><input className="field" value={draft.webhook_url ?? ''} onChange={(event) => setDraft({ ...draft, webhook_url: event.target.value })} placeholder="https://example.com/webhook" /></label>
        </Panel>

        <Panel className="p-5">
          <h3 className="mb-4 flex items-center gap-2 text-sm font-semibold"><BrainCircuit className="h-4 w-4 text-mint" /> LLM</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            <label><span className="label">Provider</span>
              <select className="field" value={draft.llm.provider} onChange={(event) => updateLLM({ provider: event.target.value })}>
                {['ollama', 'openrouter', 'openai', 'gemini', 'custom'].map((provider) => <option key={provider} value={provider}>{provider}</option>)}
              </select>
            </label>
            <label><span className="label">Preset ID</span><input className="field" value={draft.llm.preset_id} onChange={(event) => updateLLM({ preset_id: event.target.value })} /></label>
          </div>
          <label className="mt-3 block"><span className="label">Base URL</span><input className="field" value={draft.llm.base_url} onChange={(event) => updateLLM({ base_url: event.target.value })} /></label>
          <label className="mt-3 block"><span className="label">{t('apiKey')}</span><input className="field" type="password" value={draft.llm.api_key ?? ''} onChange={(event) => updateLLM({ api_key: event.target.value })} placeholder="***" /></label>
          <div className="mt-3 grid gap-3 sm:grid-cols-2">
            <label><span className="label">{t('fastModel')}</span><input className="field" value={draft.llm.fast_model} onChange={(event) => updateLLM({ fast_model: event.target.value })} /></label>
            <label><span className="label">{t('smartModel')}</span><input className="field" value={draft.llm.smart_model} onChange={(event) => updateLLM({ smart_model: event.target.value })} /></label>
            <label><span className="label">{t('visionModel')}</span><input className="field" value={draft.llm.vision_model ?? ''} onChange={(event) => updateLLM({ vision_model: event.target.value })} /></label>
            <NumberField label="Timeout sec" value={draft.llm.request_timeout_sec} onChange={(value) => updateLLM({ request_timeout_sec: value })} />
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <button className="btn" onClick={() => void testLLM()} disabled={busy}><KeyRound className="h-4 w-4" /> {t('testConnection')}</button>
            {models.length > 0 && <span className="text-xs text-slate-500">{models.slice(0, 4).join(', ')}{models.length > 4 ? '…' : ''}</span>}
          </div>
        </Panel>

        <Panel className="p-5">
          <h3 className="mb-3 flex items-center gap-2 text-sm font-semibold"><Bot className="h-4 w-4 text-violet-300" /> {t('ownerBot')}</h3>
          <label className="block"><span className="label">Bot token</span><input className="field" type="password" value={draft.owner_bot_token ?? ''} onChange={(event) => setDraft({ ...draft, owner_bot_token: event.target.value })} placeholder="***" /></label>
          <NumberField className="mt-3" label="Owner Telegram ID" value={draft.owner_telegram_id} onChange={(value) => setDraft({ ...draft, owner_telegram_id: value })} />
          <button className="btn mt-3" onClick={() => void saveBot()} disabled={busy}><Bell className="h-4 w-4" /> {t('save')}</button>
        </Panel>

        <Panel className="p-5">
          <h3 className="mb-3 flex items-center gap-2 text-sm font-semibold"><Shield className="h-4 w-4 text-amber-200" /> Offer guardrails</h3>
          <ToggleRow label={t('allowHr')} checked={draft.allow_hr_vacancies} onChange={(value) => setDraft({ ...draft, allow_hr_vacancies: value })} />
          <ToggleRow label={t('allowPartnerships')} checked={draft.allow_partnerships} onChange={(value) => setDraft({ ...draft, allow_partnerships: value })} />
          <div className="mt-4 flex flex-wrap gap-2">
            {(draft.target_languages ?? []).map((code) => <Badge key={code} tone="cyan">{code}</Badge>)}
          </div>
          <p className="mt-3 text-xs text-slate-500">Language: {language.toUpperCase()} · {draft.setup_completed ? 'Setup completed' : 'Setup pending'}</p>
        </Panel>
      </div>
    </div>
  );
}

function ToggleRow({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return <div className="flex items-center justify-between gap-3 text-sm text-slate-300"><span>{label}</span><Toggle checked={checked} onChange={onChange} /></div>;
}

function NumberField({ label, value, onChange, step = 1, className = '' }: { label: string; value: number; onChange: (value: number) => void; step?: number; className?: string }) {
  return <label className={`block ${className}`}><span className="label">{label}</span><input className="field" type="number" step={step} value={value} onChange={(event) => onChange(Number(event.target.value))} /></label>;
}
