import { useEffect, useMemo, useState } from 'react';
import { QRCodeSVG } from 'qrcode.react';
import { Bot, Check, Cpu, MemoryStick, Phone, Radar, RefreshCw, Smartphone, Sparkles, X } from 'lucide-react';
import {
  api, type AuthSessionState, type HardwarePreset, type LLMConfig, type SetupStatus,
} from '../api/client';
import { Badge, Panel, SpinnerButton, Toggle } from '../components/ui';
import { useI18n } from '../i18n';

interface Props {
  initial: SetupStatus;
  onClose: () => void;
  onComplete: () => Promise<void>;
}

export default function SetupWizard({ initial, onClose, onComplete }: Props) {
  const { t } = useI18n();
  const [step, setStep] = useState(0);
  const [preset, setPreset] = useState<HardwarePreset | null>(null);
  const [config, setConfig] = useState<LLMConfig>(initial.settings.llm);
  const [testResult, setTestResult] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [telState, setTelState] = useState<AuthSessionState | null>(null);
  const [phone, setPhone] = useState('');
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [loginMode, setLoginMode] = useState<'qr' | 'phone'>('qr');
  const [botToken, setBotToken] = useState('');
  const [ownerID, setOwnerID] = useState(String(initial.settings.owner_telegram_id || ''));
  const [autoRead, setAutoRead] = useState(initial.settings.auto_read_enabled);

  const presets = useMemo(() => initial.presets, [initial.presets]);
  const recommended = presets.find((item) => item.recommended) ?? presets[0];

  useEffect(() => {
    if (!preset && recommended) {
      setPreset(recommended);
      setConfig({
        provider: recommended.provider,
        base_url: recommended.base_url,
        api_key: initial.settings.llm.api_key ?? '',
        preset_id: recommended.id,
        fast_model: recommended.fast_model,
        smart_model: recommended.smart_model,
        vision_model: recommended.vision_model,
        request_timeout_sec: initial.settings.llm.request_timeout_sec || 45,
        max_retries: initial.settings.llm.max_retries || 2,
      });
    }
  }, [preset, recommended, initial.settings.llm]);

  useEffect(() => {
    if (!telState || !['qr_pending', 'phone_code', 'password_2fa'].includes(telState.step)) return;
    const timer = setInterval(async () => {
      try {
        const state = await api.telegramStatus();
        setTelState(state);
      } catch {
        // Keep polling quietly.
      }
    }, 1800);
    return () => clearInterval(timer);
  }, [telState]);

  const choosePreset = (item: HardwarePreset) => {
    setPreset(item);
    setConfig({
      provider: item.provider,
      base_url: item.base_url,
      api_key: config.api_key ?? '',
      preset_id: item.id,
      fast_model: item.fast_model,
      smart_model: item.smart_model,
      vision_model: item.vision_model,
      request_timeout_sec: config.request_timeout_sec || 45,
      max_retries: config.max_retries || 2,
    });
  };

  const testLLM = async () => {
    setBusy(true);
    setTestResult('');
    setError('');
    try {
      const result = await api.testLLM(config);
      setTestResult(result.ok ? `Connected${result.models?.length ? ` · ${result.models.slice(0, 5).join(', ')}` : ''}` : result.error ?? 'Connection failed');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const saveLLM = async () => {
    setBusy(true);
    setError('');
    try {
      await api.saveLLM(config);
      setStep(1);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const startQR = async () => {
    setBusy(true);
    setError('');
    try { setTelState(await api.telegramQR()); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  const submitPhone = async () => {
    setBusy(true);
    setError('');
    try { setTelState(await api.telegramPhone(phone)); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  const submitCode = async () => {
    setBusy(true);
    setError('');
    try { setTelState(await api.telegramCode(code)); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  const submitPassword = async () => {
    setBusy(true);
    setError('');
    try { setTelState(await api.telegramPassword(password)); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  const importChats = async () => {
    setBusy(true);
    try { await api.importDialogs(); setError(''); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  const complete = async () => {
    setBusy(true);
    setError('');
    try {
      const current = await api.getSettings();
      if (current.auto_read_enabled !== autoRead) {
        await api.saveSettings({ ...current, auto_read_enabled: autoRead });
      }
      if (botToken.trim() || ownerID) await api.saveBot(botToken, Number(ownerID) || 0);
      await api.completeSetup();
      await onComplete();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 overflow-y-auto bg-ink/80 p-3 backdrop-blur-md sm:p-6">
      <div className="mx-auto max-w-4xl">
        <Panel className="overflow-hidden">
          <div className="flex items-center justify-between border-b border-white/[0.06] p-5">
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-mint/30 bg-mint/10"><Radar className="h-5 w-5 text-mint" /></div>
              <div><h1 className="text-lg font-semibold">{t('setup')}</h1><p className="text-xs text-slate-500">{t('setupSubtitle')}</p></div>
            </div>
            <button className="btn h-9 w-9 p-0" onClick={onClose} aria-label={t('cancel')}><X className="h-4 w-4" /></button>
          </div>

          <div className="flex items-center gap-1 border-b border-white/[0.06] px-4 py-2">
            {['LLM', 'Telegram', 'Owner bot'].map((label, index) => (
              <button key={label} className={`flex flex-1 items-center justify-center gap-2 rounded-lg px-2 py-2 text-xs ${step === index ? 'bg-mint/10 text-mint' : 'text-slate-500'}`} onClick={() => setStep(index)}>
                <span className={`flex h-5 w-5 items-center justify-center rounded-full border text-[10px] ${step >= index ? 'border-mint/40 text-mint' : 'border-white/10'}`}>{step > index ? <Check className="h-3 w-3" /> : index + 1}</span>
                {label}
              </button>
            ))}
          </div>

          <div className="p-5">
            {error && <p className="mb-4 rounded-xl border border-rose-400/20 bg-rose-400/10 p-3 text-sm text-rose-200">{error}</p>}

            {step === 0 && (
              <div>
                <div className="mb-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
                  <HardwareStat icon={MemoryStick} label="RAM" value={`${initial.hardware.ram_gb} GB`} />
                  <HardwareStat icon={Cpu} label="CPU" value={`${initial.hardware.cpu_cores} cores`} />
                  <HardwareStat icon={Sparkles} label={t('recommended')} value={initial.hardware.recommended_tier} />
                  <HardwareStat icon={Radar} label="OS" value={`${initial.hardware.os}/${initial.hardware.arch}`} />
                </div>
                <div className="grid gap-2 sm:grid-cols-2">
                  {presets.map((item) => (
                    <button key={item.id} onClick={() => choosePreset(item)} className={`rounded-xl border p-3 text-left transition-colors duration-150 ${preset?.id === item.id ? 'border-mint/40 bg-mint/[0.07]' : 'border-white/[0.06] bg-white/[0.02] hover:border-white/15'}`}>
                      <div className="flex items-center gap-2"><span className="text-sm font-medium">{item.title}</span>{item.recommended && <Badge tone="mint">{t('recommended')}</Badge>}{item.requires_key && <Badge tone="amber">BYOK</Badge>}</div>
                      <p className="mt-1 line-clamp-2 text-xs text-slate-500">{item.description}</p>
                      <p className="mt-1 text-[10px] text-slate-600">{item.fast_model} · {item.tier}</p>
                    </button>
                  ))}
                </div>
                <div className="mt-4 grid gap-3 sm:grid-cols-2">
                  <label><span className="label">Base URL</span><input className="field" value={config.base_url} onChange={(event) => setConfig({ ...config, base_url: event.target.value })} /></label>
                  <label><span className="label">API key</span><input className="field" type="password" value={config.api_key ?? ''} onChange={(event) => setConfig({ ...config, api_key: event.target.value })} placeholder="optional for Ollama" /></label>
                  <label><span className="label">Fast model</span><input className="field" value={config.fast_model} onChange={(event) => setConfig({ ...config, fast_model: event.target.value, smart_model: event.target.value })} /></label>
                  <label><span className="label">Smart model</span><input className="field" value={config.smart_model} onChange={(event) => setConfig({ ...config, smart_model: event.target.value })} /></label>
                </div>
                {testResult && <p className="mt-3 rounded-lg border border-white/10 bg-white/[0.03] p-2 text-xs text-slate-300">{testResult}</p>}
                <div className="mt-4 flex flex-wrap gap-2">
                  <button className="btn" disabled={busy} onClick={() => void testLLM()}>{t('testConnection')}</button>
                  <SpinnerButton loading={busy} className="btn-primary ml-auto" onClick={() => void saveLLM()}>{t('next')}</SpinnerButton>
                </div>
              </div>
            )}

            {step === 1 && (
              <div>
                <div className="mb-4 flex gap-2">
                  <button className={`btn ${loginMode === 'qr' ? 'border-mint/30 text-mint' : ''}`} onClick={() => setLoginMode('qr')}><Smartphone className="h-4 w-4" /> QR</button>
                  <button className={`btn ${loginMode === 'phone' ? 'border-mint/30 text-mint' : ''}`} onClick={() => setLoginMode('phone')}><Phone className="h-4 w-4" /> Phone</button>
                </div>
                {loginMode === 'qr' ? (
                  <div className="flex flex-col items-center gap-3 py-4">
                    {telState?.qr_url ? (
                      <div className="rounded-2xl bg-white p-4"><QRCodeSVG value={telState.qr_url} size={220} level="M" /></div>
                    ) : (
                      <div className="flex h-52 w-52 items-center justify-center rounded-2xl border border-dashed border-white/10 text-xs text-slate-500">QR appears here</div>
                    )}
                    <button className="btn" disabled={busy} onClick={() => void startQR()}><RefreshCw className="h-4 w-4" /> {telState?.qr_url ? 'Refresh QR' : 'Show QR'}</button>
                    {telState && <Badge tone={telState.step === 'authorized' ? 'mint' : 'cyan'}>{telState.step}</Badge>}
                  </div>
                ) : (
                  <div className="space-y-3">
                    {(!telState || telState.step === 'idle') && (
                      <div className="flex gap-2"><input className="field" value={phone} onChange={(event) => setPhone(event.target.value)} placeholder="+1 555 000 0000" /><button className="btn" disabled={busy || !phone} onClick={() => void submitPhone()}>{t('next')}</button></div>
                    )}
                    {telState?.step === 'phone_code' && (
                      <div className="flex gap-2"><input className="field" value={code} onChange={(event) => setCode(event.target.value)} placeholder={t('code')} /><button className="btn" disabled={busy || !code} onClick={() => void submitCode()}>{t('next')}</button></div>
                    )}
                    {telState?.step === 'password_2fa' && (
                      <div className="flex gap-2"><input className="field" type="password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder={t('password')} /><button className="btn" disabled={busy || !password} onClick={() => void submitPassword()}>{t('next')}</button></div>
                    )}
                    {telState && <p className="text-xs text-slate-500">{telState.phone_masked} · {telState.step} {telState.error && `· ${telState.error}`}</p>}
                  </div>
                )}
                <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-white/[0.06] pt-4">
                  <button className="btn" disabled={busy} onClick={() => void importChats()}>{t('addMyChats')}</button>
                  <span className="text-xs text-slate-500">Optional — can also do this later.</span>
                  <button className="btn-primary ml-auto" onClick={() => setStep(2)}>{t('next')}</button>
                </div>
              </div>
            )}

            {step === 2 && (
              <div>
                <h3 className="mb-2 flex items-center gap-2 text-sm font-semibold"><Bot className="h-4 w-4 text-violet-300" /> {t('optionalBot')}</h3>
                <p className="mb-4 text-xs text-slate-500">Owner-only control bot for lead alerts, /status, /pause, /resume and funnel summaries.</p>
                <div className="grid gap-3 sm:grid-cols-2">
                  <label><span className="label">Bot token</span><input className="field" type="password" value={botToken} onChange={(event) => setBotToken(event.target.value)} placeholder="123456:ABC..." /></label>
                  <label><span className="label">Owner Telegram ID</span><input className="field" value={ownerID} onChange={(event) => setOwnerID(event.target.value)} placeholder="123456789" /></label>
                </div>
                <div className="mt-3 flex items-center justify-between rounded-xl border border-white/10 bg-white/[0.02] p-3 text-sm"><span>Auto-read history (anti-ban)</span><Toggle checked={autoRead} onChange={setAutoRead} /></div>
                <div className="mt-5 flex items-center gap-2">
                  <button className="btn" onClick={() => setStep(1)}>{t('back')}</button>
                  <SpinnerButton loading={busy} className="btn-primary ml-auto" onClick={() => void complete()}>{t('finish')}</SpinnerButton>
                </div>
              </div>
            )}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function HardwareStat({ icon: Icon, label, value }: { icon: typeof Cpu; label: string; value: string }) {
  return (
    <div className="rounded-xl border border-white/[0.06] bg-white/[0.02] p-2.5">
      <div className="flex items-center gap-1.5 text-[10px] uppercase tracking-wider text-slate-500"><Icon className="h-3 w-3" />{label}</div>
      <p className="mt-1 truncate text-xs font-medium text-slate-200">{value}</p>
    </div>
  );
}
