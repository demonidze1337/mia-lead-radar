import { useCallback, useEffect, useMemo, useState, type ReactElement } from 'react';
import WebApp from '@twa-dev/sdk';
import { motion } from 'framer-motion';
import {
  BarChart3, KeyRound, LayoutDashboard, LogOut, Menu, MessageSquare, Radar, Radio,
  Settings, Sparkles, UserRound, Wand2, X,
} from 'lucide-react';
import { api, ApiError, type SetupStatus } from './api/client';
import { AppContext, type AppContextValue, type ViewKey } from './appContext';
import { isTelegramMiniApp } from './utils/telegramLinks';
import { LANGUAGES, useI18n, type Language } from './i18n';
import { Loading } from './components/ui';
import DashboardView from './views/DashboardView';
import LeadsView from './views/LeadsView';
import RadarView from './views/RadarView';
import TerminalView from './views/TerminalView';
import ConsultantView from './views/ConsultantView';
import ChatsView from './views/ChatsView';
import AnalyticsView from './views/AnalyticsView';
import SettingsView from './views/SettingsView';
import SetupWizard from './views/SetupWizard';

const NAV: { key: ViewKey; icon: typeof LayoutDashboard; labelKey: 'dashboard' | 'leads' | 'radar' | 'terminal' | 'consultant' | 'chats' | 'analytics' | 'settings' }[] = [
  { key: 'dashboard', icon: LayoutDashboard, labelKey: 'dashboard' },
  { key: 'leads', icon: MessageSquare, labelKey: 'leads' },
  { key: 'radar', icon: Radar, labelKey: 'radar' },
  { key: 'terminal', icon: Radio, labelKey: 'terminal' },
  { key: 'consultant', icon: Sparkles, labelKey: 'consultant' },
  { key: 'chats', icon: UserRound, labelKey: 'chats' },
  { key: 'analytics', icon: BarChart3, labelKey: 'analytics' },
  { key: 'settings', icon: Settings, labelKey: 'settings' },
];

export default function App() {
  const { t } = useI18n();
  const [phase, setPhase] = useState<'loading' | 'login' | 'app'>('loading');
  const [setup, setSetup] = useState<SetupStatus | null>(null);
  const [view, setView] = useState<ViewKey>('dashboard');
  const [setupOpen, setSetupOpen] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [loginError, setLoginError] = useState('');
  const [accessCode, setAccessCode] = useState('');
  const [loginBusy, setLoginBusy] = useState(false);

  const loadSetup = useCallback(async () => {
    const status = await api.setupStatus();
    setSetup(status);
    if (!status.setup_completed) setSetupOpen(true);
    return status;
  }, []);

  const bootstrap = useCallback(async () => {
    try {
      const auth = await api.authStatus();
      if (!auth.authenticated) {
        if (isTelegramMiniApp()) {
          await api.authTelegram(WebApp.initData);
        } else {
          await api.authLocal('');
        }
      }
      const status = await loadSetup();
      setPhase('app');
      if (!status.setup_completed) setSetupOpen(true);
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) setPhase('login');
      else {
        setLoginError(error instanceof Error ? error.message : String(error));
        setPhase('login');
      }
    }
  }, [loadSetup]);

  useEffect(() => { void bootstrap(); }, [bootstrap]);

  useEffect(() => {
    try {
      if (isTelegramMiniApp()) {
        WebApp.ready();
        WebApp.expand();
        WebApp.setHeaderColor('#070d14');
      }
    } catch {
      // Browser mode.
    }
  }, []);

  const submitLocal = async () => {
    setLoginBusy(true);
    setLoginError('');
    try {
      await api.authLocal(accessCode);
      const status = await loadSetup();
      setPhase('app');
      if (!status.setup_completed) setSetupOpen(true);
    } catch (error) {
      setLoginError(error instanceof Error ? error.message : String(error));
    } finally {
      setLoginBusy(false);
    }
  };

  const refreshSetup = useCallback(async () => { await loadSetup(); }, [loadSetup]);
  const refreshSettings = useCallback(async () => {
    const settings = await api.getSettings();
    setSetup((current) => (current ? { ...current, settings } : current));
  }, []);

  const logout = useCallback(async () => {
    try { await api.logout(); } catch { /* Ignore network errors on logout. */ }
    setPhase('login');
    setSetup(null);
  }, []);

  const context = useMemo<AppContextValue | null>(() => {
    if (!setup) return null;
    return {
      setup,
      settings: setup.settings,
      refreshSetup,
      refreshSettings,
      navigate: setView,
      openSetup: () => setSetupOpen(true),
      logout,
    };
  }, [setup, refreshSetup, refreshSettings, logout]);

  if (phase === 'loading') {
    return <div className="relative z-10 flex min-h-screen items-center justify-center"><Loading label={t('loading')} /></div>;
  }

  if (phase === 'login') {
    return (
      <div className="relative z-10 flex min-h-screen items-center justify-center p-4">
        <div className="mesh-glow" />
        <motion.div initial={{ opacity: 0, y: 16 }} animate={{ opacity: 1, y: 0 }} className="panel w-full max-w-md p-6">
          <div className="mb-6 flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-2xl border border-mint/30 bg-mint/10">
              <Radar className="h-6 w-6 text-mint" />
            </div>
            <div>
              <h1 className="text-lg font-semibold">{t('appName')}</h1>
              <p className="text-xs text-slate-400">{t('tagline')}</p>
            </div>
          </div>
          <h2 className="mb-1 text-base font-medium">{t('localAccess')}</h2>
          <p className="mb-5 text-sm text-slate-400">{t('accessCode')}</p>
          <div className="space-y-3">
            <input
              className="field"
              type="password"
              value={accessCode}
              onChange={(event) => setAccessCode(event.target.value)}
              onKeyDown={(event) => { if (event.key === 'Enter') void submitLocal(); }}
              placeholder={t('accessCode')}
              autoFocus
            />
            <button className="btn-primary w-full" onClick={() => void submitLocal()} disabled={loginBusy}>
              <KeyRound className="h-4 w-4" /> {t('signIn')}
            </button>
            {loginError && <p className="text-sm text-rose-300">{loginError}</p>}
          </div>
          <LanguagePicker className="mt-6" />
        </motion.div>
      </div>
    );
  }

  if (!context) return <div className="relative z-10 flex min-h-screen items-center justify-center"><Loading /></div>;
  const active = NAV.find((item) => item.key === view) ?? NAV[0];
  const View = viewComponent(view);

  return (
    <AppContext.Provider value={context}>
      <div className="mesh-glow" />
      <div className="relative z-10 flex min-h-screen">
        <aside className="sticky top-0 hidden h-screen w-64 shrink-0 flex-col border-r border-white/[0.06] bg-ink/70 p-4 backdrop-blur-xl lg:flex">
          <Brand />
          <nav className="mt-6 space-y-1">
            {NAV.map((item) => (
              <button
                key={item.key}
                onClick={() => setView(item.key)}
                className={`flex w-full items-center gap-3 rounded-xl px-3 py-2 text-left text-sm transition-colors duration-150 ${
                  view === item.key ? 'bg-mint/10 text-mint' : 'text-slate-400 hover:bg-white/[0.04] hover:text-slate-100'
                }`}
              >
                <item.icon className="h-4 w-4" />
                {t(item.labelKey)}
              </button>
            ))}
          </nav>
          <div className="mt-auto space-y-2">
            <button className="btn w-full justify-start" onClick={() => setSetupOpen(true)}>
              <Wand2 className="h-4 w-4" /> {t('openSetup')}
            </button>
            <button className="btn w-full justify-start text-slate-400" onClick={() => void logout()}>
              <LogOut className="h-4 w-4" /> {t('logout')}
            </button>
          </div>
        </aside>

        <main className="min-w-0 flex-1">
          <header className="sticky top-0 z-20 flex items-center justify-between border-b border-white/[0.06] bg-ink/75 px-4 py-3 backdrop-blur-xl lg:px-8">
            <div className="flex items-center gap-3">
              <button
                className="btn h-9 w-9 p-0 lg:hidden"
                onClick={() => setMenuOpen(true)}
                aria-label={t('menu')}
              >
                <Menu className="h-4 w-4" />
              </button>
              <div className="lg:hidden"><Brand compact /></div>
              <div>
                <h2 className="text-sm font-semibold text-slate-100">{t(active.labelKey)}</h2>
                <p className="hidden text-[11px] text-slate-500 sm:block">{t('tagline')}</p>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <LanguagePicker compact />
              <button className="btn h-9 w-9 p-0 lg:hidden" onClick={() => setSetupOpen(true)} aria-label={t('openSetup')}>
                <Wand2 className="h-4 w-4" />
              </button>
            </div>
          </header>

          <div className="mx-auto w-full max-w-[1500px] px-4 py-5 pb-24 lg:px-8 lg:pb-10">
            <View />
          </div>

          <nav className="safe-bottom fixed inset-x-0 bottom-0 z-30 border-t border-white/[0.08] bg-ink/90 px-2 pt-1.5 backdrop-blur-xl lg:hidden">
            <div className="grid grid-cols-5">
              {NAV.slice(0, 5).map((item) => (
                <button
                  key={item.key}
                  onClick={() => setView(item.key)}
                  className={`flex flex-col items-center gap-1 rounded-lg py-1.5 text-[10px] ${view === item.key ? 'text-mint' : 'text-slate-500'}`}
                >
                  <item.icon className="h-5 w-5" />
                  {t(item.labelKey)}
                </button>
              ))}
            </div>
          </nav>
        </main>
      </div>

      {menuOpen && (
        <div className="fixed inset-0 z-40 lg:hidden" onClick={() => setMenuOpen(false)}>
          <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" />
          <motion.aside
            initial={{ x: -300 }}
            animate={{ x: 0 }}
            transition={{ duration: 0.18 }}
            className="relative flex h-full w-72 max-w-[85vw] flex-col border-r border-white/10 bg-ink p-4 shadow-2xl"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <Brand />
              <button className="btn h-8 w-8 p-0" onClick={() => setMenuOpen(false)} aria-label={t('cancel')}>
                <X className="h-4 w-4" />
              </button>
            </div>
            <nav className="mt-5 space-y-1">
              {NAV.map((item) => (
                <button
                  key={item.key}
                  onClick={() => { setView(item.key); setMenuOpen(false); }}
                  className={`flex w-full items-center gap-3 rounded-xl px-3 py-2 text-left text-sm transition-colors duration-150 ${
                    view === item.key ? 'bg-mint/10 text-mint' : 'text-slate-400 hover:bg-white/[0.04] hover:text-slate-100'
                  }`}
                >
                  <item.icon className="h-4 w-4" />
                  {t(item.labelKey)}
                </button>
              ))}
            </nav>
            <div className="mt-auto space-y-2">
              <button className="btn w-full justify-start" onClick={() => { setMenuOpen(false); setSetupOpen(true); }}>
                <Wand2 className="h-4 w-4" /> {t('openSetup')}
              </button>
              <button className="btn w-full justify-start text-slate-400" onClick={() => { setMenuOpen(false); void logout(); }}>
                <LogOut className="h-4 w-4" /> {t('logout')}
              </button>
            </div>
          </motion.aside>
        </div>
      )}

      {setupOpen && setup && (
        <SetupWizard
          initial={setup}
          onClose={() => setSetupOpen(false)}
          onComplete={async () => {
            await loadSetup();
            setSetupOpen(false);
            setPhase('app');
          }}
        />
      )}
    </AppContext.Provider>
  );
}

function Brand({ compact = false }: { compact?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="flex items-center gap-3">
      <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-mint/30 bg-mint/10">
        <Radar className="h-5 w-5 text-mint" />
      </div>
      {!compact && (
        <div>
          <p className="text-sm font-semibold tracking-tight">{t('appName')}</p>
          <p className="text-[10px] uppercase tracking-[0.18em] text-slate-500">Self-hosted</p>
        </div>
      )}
    </div>
  );
}

function LanguagePicker({ compact = false, className = '' }: { compact?: boolean; className?: string }) {
  const { t, language, setLanguage } = useI18n();
  return (
    <label className={`flex items-center gap-2 text-xs text-slate-400 ${className}`}>
      {!compact && <span>{t('language')}</span>}
      <select
        className={`rounded-lg border border-white/10 bg-ink/80 px-2 py-1 text-xs text-slate-200 outline-none ${compact ? 'max-w-[7.5rem]' : ''}`}
        value={language}
        onChange={(event) => setLanguage(event.target.value as Language)}
      >
        {LANGUAGES.map((item) => <option key={item.code} value={item.code}>{item.label}</option>)}
      </select>
    </label>
  );
}

function viewComponent(view: ViewKey): () => ReactElement {
  switch (view) {
    case 'leads': return LeadsView;
    case 'radar': return RadarView;
    case 'terminal': return TerminalView;
    case 'consultant': return ConsultantView;
    case 'chats': return ChatsView;
    case 'analytics': return AnalyticsView;
    case 'settings': return SettingsView;
    default: return DashboardView;
  }
}

