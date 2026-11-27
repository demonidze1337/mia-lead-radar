export type HardwareTier = 'weak' | 'balanced' | 'workstation' | 'byok';
export type ChatStatus = 'queued' | 'validating' | 'joined' | 'requires_approval' | 'bad' | 'banned' | 'skipped';
export type LeadCategory = 'trash' | 'doubtful' | 'potential' | 'clear';
export type LeadStatus = 'new' | 'in_progress' | 'replied' | 'closed' | 'false_positive' | 'hidden';
export type RadarTagKind = 'root' | 'live';

export interface HardwareProfile {
  ram_bytes: number;
  ram_gb: number;
  cpu_cores: number;
  os: string;
  arch: string;
  recommended_tier: HardwareTier;
}

export interface LLMConfig {
  provider: string;
  base_url: string;
  api_key?: string;
  preset_id: string;
  fast_model: string;
  smart_model: string;
  vision_model?: string;
  request_timeout_sec: number;
  max_retries: number;
}

export interface RuntimeSettings {
  ui_language: string;
  target_languages: string[];
  llm: LLMConfig;
  owner_bot_token?: string;
  owner_telegram_id: number;
  auto_read_enabled: boolean;
  auto_join_enabled: boolean;
  allow_hr_vacancies: boolean;
  allow_partnerships: boolean;
  min_lead_score: number;
  sound_enabled: boolean;
  haptic_enabled: boolean;
  noise_guard_enabled: boolean;
  noise_trash_threshold_pct: number;
  webhook_url?: string;
  setup_completed: boolean;
}

export interface HardwarePreset {
  id: string;
  tier: HardwareTier;
  provider: string;
  title: string;
  description: string;
  min_ram_gb: number;
  base_url: string;
  fast_model: string;
  smart_model: string;
  vision_model?: string;
  recommended: boolean;
  requires_key: boolean;
}

export interface RadarTag {
  phrase: string;
  kind: RadarTagKind;
  weight: number;
  enabled: boolean;
}

export interface RadarProfile {
  id: number;
  name: string;
  description: string;
  tags: RadarTag[];
  target_languages: string[];
  stop_phrases: string[];
  learned_stop_words: string[];
  allow_hr_vacancies: boolean;
  allow_partnerships: boolean;
  quality_score: number;
  quality_notes: string;
  version: number;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface RadarVersion {
  id: number;
  radar_id: number;
  version: number;
  description: string;
  tags: RadarTag[];
  stop_phrases: string[];
  learned_stop_words: string[];
  change_source: string;
  audit_summary: string;
  created_at: string;
}

export interface Lead {
  id: number;
  radar_id: number;
  chat_id: number;
  tg_chat_id: number;
  chat_title: string;
  chat_username?: string;
  msg_id: number;
  msg_link: string;
  author_id: number;
  author_username?: string;
  author_name: string;
  text: string;
  lang: string;
  score: number;
  category: LeadCategory;
  status: LeadStatus;
  reason: string;
  matched_tags: string[];
  tier0_score: number;
  llm_model: string;
  icebreakers: string[];
  viewed_at?: string;
  profile_opened_at?: string;
  icebreaker_count: number;
  saved_time_seconds: number;
  created_at: string;
  updated_at: string;
}

export interface LeadList {
  leads: Lead[];
  total: number;
  limit: number;
  offset: number;
}

export interface Chat {
  id: number;
  tg_chat_id: number;
  access_hash?: number;
  username?: string;
  invite_hash?: string;
  title: string;
  chat_type: string;
  source: string;
  status: ChatStatus;
  member_count: number;
  msg_velocity_per_hour: number;
  has_comments: boolean;
  linked_chat_id?: number;
  assigned_account_id?: number;
  last_message_id: number;
  last_read_at?: string;
  last_scanned_at?: string;
  next_retry_at?: string;
  error_reason?: string;
  created_at: string;
  updated_at: string;
}

export interface TelegramAccount {
  id: number;
  label: string;
  phone_masked: string;
  tg_user_id: number;
  username?: string;
  first_name?: string;
  status: string;
  flood_until?: string;
  joined_chats_count: number;
  daily_joins_count: number;
  daily_joins_reset_at?: string;
  last_active_at?: string;
  last_error?: string;
  created_at: string;
  updated_at: string;
}

export interface PipelineStats {
  running: boolean;
  paused: boolean;
  active_accounts: number;
  flooded_accounts: number;
  processed_msgs: number;
  qualified_leads: number;
  last_error?: string;
}

export interface AuthSessionState {
  session_key: string;
  account_id?: number;
  step: string;
  qr_url?: string;
  phone_masked?: string;
  tg_user_id?: number;
  username?: string;
  first_name?: string;
  error?: string;
  updated_at: string;
}

export interface AuthStatus {
  authenticated: boolean;
  setup_completed: boolean;
  auth?: { method: string; tg_user_id?: number; role: string; label?: string };
}

export interface SetupStatus {
  hardware: HardwareProfile;
  presets: HardwarePreset[];
  settings: RuntimeSettings;
  accounts: TelegramAccount[];
  has_active_radar: boolean;
  setup_completed: boolean;
}

export interface SignalBreakdown {
  bm25_score: number;
  intent_score: number;
  diy_fatigue_score: number;
  seller_penalty: number;
  hr_penalty: number;
  spam_penalty: number;
  stop_word_penalty: number;
  matched_intent_hits?: string[];
  matched_seller_hits?: string[];
  matched_hr_hits?: string[];
}

export interface EvalResult {
  passed: boolean;
  score: number;
  reject_reason?: string;
  detected_lang: string;
  lang_confidence: number;
  matched_tags?: string[];
  throttled_tags?: string[];
  breakdown: SignalBreakdown;
  fingerprint: string;
  latency_micros: number;
}

export interface QualifyResult {
  score: number;
  is_lead: boolean;
  category: string;
  lang: string;
  reason: string;
  matched_concepts: string[];
}

export interface SimulateResult {
  eval: EvalResult;
  qualify?: QualifyResult;
  llm_error?: string;
}

export interface SuggestedTag {
  phrase: string;
  kind: string;
  weight: number;
}

export interface PhraseSuggestResult {
  tags: SuggestedTag[];
  stop_phrases: string[];
  quality_score: number;
  quality_notes: string;
}

export interface IcebreakerResult {
  lang: string;
  drafts: string[];
}

export interface RadarPatch {
  apply: boolean;
  description?: string;
  tags?: RadarTag[];
  stop_phrases?: string[];
  summary?: string;
}

export interface AuditResult {
  reply: string;
  diagnosis: string;
  recommendations: string[];
  apply_radar_settings?: RadarPatch;
}

export interface RoleplayReply {
  message: string;
  mood: string;
  hidden_note: string;
}

export interface RoleplayReview {
  score: number;
  strengths: string[];
  weaknesses: string[];
  improved_text: string;
}

export interface FunnelSummary {
  radar_id: number;
  days: number;
  messages_seen: number;
  tier0_passed: number;
  tier0_rejected: number;
  llm_called: number;
  trash_count: number;
  doubtful_count: number;
  potential_count: number;
  clear_count: number;
  total_leads: number;
  viewed_count: number;
  profile_opened_count: number;
  icebreaker_count: number;
  false_positive_count: number;
  saved_time_seconds: number;
  trash_ratio_pct: number;
  view_conversion_pct: number;
  profile_conversion_pct: number;
  noisy_tags: TagStat[];
  noise_guard_triggered: boolean;
}

export interface DailyMetric {
  date: string;
  radar_id: number;
  messages_seen: number;
  tier0_passed: number;
  tier0_rejected: number;
  llm_called: number;
  trash_count: number;
  doubtful_count: number;
  potential_count: number;
  clear_count: number;
  viewed_count: number;
  profile_opened_count: number;
  icebreaker_count: number;
  false_positive_count: number;
  saved_time_seconds: number;
}

export interface TagStat {
  radar_id: number;
  tag: string;
  total_hits: number;
  trash_hits: number;
  lead_hits: number;
  false_positive_hits: number;
  throttled_until?: string;
  updated_at: string;
}

export interface TrashSample {
  id: number;
  radar_id: number;
  source: string;
  reason: string;
  matched_tags: string[];
  score: number;
  chat_title: string;
  author_id: number;
  text_excerpt: string;
  lang: string;
  created_at: string;
}

export interface NoiseAlert {
  radar_id: number;
  trash_ratio_pct: number;
  threshold_pct: number;
  throttled_tags: string[];
  noisy_tags: TagStat[];
  recommended_action: string;
  triggered_at: string;
}

export interface ConsultantMessage {
  id: number;
  session_key: string;
  role: string;
  mode: string;
  lead_id?: number;
  content: string;
  metadata_json?: string;
  created_at: string;
}

export interface TerminalEvent {
  id: number;
  type: string;
  level: 'info' | 'warn' | 'error' | 'success' | string;
  message: string;
  data?: Record<string, unknown>;
  timestamp: string;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    credentials: 'same-origin',
    ...options,
    headers: {
      ...(options.body ? { 'Content-Type': 'application/json' } : {}),
      ...(options.headers ?? {}),
    },
  });
  const contentType = response.headers.get('content-type') ?? '';
  if (!response.ok) {
    let message = response.statusText;
    if (contentType.includes('application/json')) {
      const payload = (await response.json().catch(() => ({}))) as { error?: string };
      message = payload.error ?? message;
    }
    throw new ApiError(response.status, message);
  }
  if (response.status === 204) return undefined as T;
  if (contentType.includes('application/json')) return (await response.json()) as T;
  return (await response.text()) as T;
}

const json = (method: string, body?: unknown): RequestInit => ({
  method,
  body: body === undefined ? undefined : JSON.stringify(body),
});

export const api = {
  health: () => request<{ status: string }>('/api/health'),
  authStatus: () => request<AuthStatus>('/api/auth/status'),
  authTelegram: (initData: string) =>
    request<{ token: string }>('/api/auth/telegram', json('POST', { init_data: initData })),
  authLocal: (accessCode = '') =>
    request<{ token: string }>('/api/auth/local', json('POST', { access_code: accessCode })),
  logout: () => request<unknown>('/api/auth/logout', json('POST', {})),

  setupStatus: () => request<SetupStatus>('/api/setup/status'),
  testLLM: (config: LLMConfig) =>
    request<{ ok: boolean; error?: string; models?: string[] }>('/api/setup/llm/test', json('POST', config)),
  saveLLM: (config: LLMConfig) => request<{ saved: boolean }>('/api/setup/llm/save', json('POST', config)),
  telegramQR: () => request<AuthSessionState>('/api/setup/telegram/qr', json('POST', {})),
  telegramPhone: (phone: string) =>
    request<AuthSessionState>('/api/setup/telegram/phone', json('POST', { phone })),
  telegramCode: (code: string) =>
    request<AuthSessionState>('/api/setup/telegram/code', json('POST', { code })),
  telegramPassword: (password: string) =>
    request<AuthSessionState>('/api/setup/telegram/password', json('POST', { password })),
  telegramStatus: () => request<AuthSessionState>('/api/setup/telegram/status'),
  saveBot: (botToken: string, ownerTelegramId: number) =>
    request<unknown>('/api/setup/bot', json('POST', { bot_token: botToken, owner_telegram_id: ownerTelegramId })),
  completeSetup: () => request<unknown>('/api/setup/complete', json('POST', {})),

  getSettings: () => request<RuntimeSettings>('/api/settings'),
  saveSettings: (settings: RuntimeSettings) => request<RuntimeSettings>('/api/settings', json('PUT', settings)),

  listRadars: () => request<RadarProfile[]>('/api/radars'),
  createRadar: (radar: Partial<RadarProfile>) => request<RadarProfile>('/api/radars', json('POST', radar)),
  updateRadar: (radar: RadarProfile) =>
    request<RadarProfile>(`/api/radars/${radar.id}`, json('PUT', radar)),
  radarVersions: (id: number) => request<RadarVersion[]>(`/api/radars/${id}/versions?limit=30`),
  suggestRadar: (description: string, targetLanguages: string[], uiLanguage: string) =>
    request<PhraseSuggestResult>(
      '/api/radars/suggest',
      json('POST', { description, target_languages: targetLanguages, ui_language: uiLanguage }),
    ),
  simulate: (payload: {
    radar_id: number;
    message: string;
    use_llm: boolean;
    author_id?: number;
    author_name?: string;
    chat_id?: number;
    msg_id?: number;
  }) => request<SimulateResult>('/api/radars/simulate', json('POST', payload)),

  listLeads: (params: Record<string, string | number | boolean | undefined>) => {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== '') query.set(key, String(value));
    });
    return request<LeadList>(`/api/leads?${query.toString()}`);
  },
  leadStatus: (id: number, status: LeadStatus) =>
    request<Lead>(`/api/leads/${id}/status`, json('PATCH', { status })),
  leadViewed: (id: number) => request<unknown>(`/api/leads/${id}/view`, json('POST', {})),
  leadProfileOpened: (id: number) => request<unknown>(`/api/leads/${id}/profile-opened`, json('POST', {})),
  leadFalsePositive: (id: number, learnStopWords: boolean) =>
    request<unknown>(`/api/leads/${id}/false-positive`, json('POST', { learn_stop_words: learnStopWords })),
  leadIcebreaker: (id: number, style: string) =>
    request<IcebreakerResult>(`/api/leads/${id}/icebreaker`, json('POST', { style })),
  exportLeadsURL: (format: 'csv' | 'json', radarId?: number) =>
    `/api/leads/export?format=${format}${radarId ? `&radar_id=${radarId}` : ''}`,

  listChats: (params: Record<string, string | number | undefined> = {}) => {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== '') query.set(key, String(value));
    });
    return request<Chat[]>(`/api/chats?${query.toString()}`);
  },
  importDialogs: () => request<{ imported: number }>('/api/chats/import-dialogs', json('POST', {})),
  enqueueLinks: (links: string[]) =>
    request<{ inserted: number }>('/api/chats/enqueue', json('POST', { links })),
  listAccounts: () => request<TelegramAccount[]>('/api/accounts'),
  clearFlood: (id: number) => request<unknown>(`/api/accounts/${id}/clear-flood`, json('POST', {})),
  deleteAccount: (id: number) => request<unknown>(`/api/accounts/${id}`, { method: 'DELETE' }),
  pipelineStats: () => request<PipelineStats>('/api/pipeline/stats'),
  pausePipeline: () => request<unknown>('/api/pipeline/pause', json('POST', {})),
  resumePipeline: () => request<unknown>('/api/pipeline/resume', json('POST', {})),

  audit: (radarId: number, question: string) =>
    request<AuditResult>('/api/consultant/audit', json('POST', { radar_id: radarId, question })),
  applyPatch: (radarId: number, patch: RadarPatch) =>
    request<RadarProfile>('/api/consultant/apply-patch', json('POST', { radar_id: radarId, patch })),
  roleplay: (radarId: number, leadId: number, sellerReply: string) =>
    request<RoleplayReply>('/api/consultant/roleplay', json('POST', { radar_id: radarId, lead_id: leadId, seller_reply: sellerReply })),
  reviewRoleplay: (leadId: number) =>
    request<RoleplayReview>('/api/consultant/roleplay/review', json('POST', { lead_id: leadId })),
  consultantHistory: (sessionKey: string) =>
    request<ConsultantMessage[]>(`/api/consultant/history?session_key=${encodeURIComponent(sessionKey)}`),

  funnel: (radarId?: number, days = 7) =>
    request<FunnelSummary>(`/api/analytics/funnel?days=${days}${radarId ? `&radar_id=${radarId}` : ''}`),
  daily: (radarId?: number, days = 30) =>
    request<DailyMetric[]>(`/api/analytics/daily?days=${days}${radarId ? `&radar_id=${radarId}` : ''}`),
  trashSamples: (radarId?: number) =>
    request<TrashSample[]>(`/api/analytics/trash-samples?limit=25${radarId ? `&radar_id=${radarId}` : ''}`),
  tagStats: (radarId?: number) => request<TagStat[]>(`/api/analytics/tags${radarId ? `?radar_id=${radarId}` : ''}`),
  checkNoiseGuard: (radarId: number) =>
    request<{ alert: NoiseAlert | null; triggered: boolean }>('/api/analytics/noise-guard/check', json('POST', { radar_id: radarId })),
  throttleTag: (radarId: number, tag: string, until?: string, clear = false) =>
    request<unknown>('/api/analytics/tags/throttle', json('POST', { radar_id: radarId, tag, until, clear })),
};

export function connectTerminal(
  handlers: {
    onEvent: (event: TerminalEvent) => void;
    onStatus?: (status: 'open' | 'closed' | 'error') => void;
  },
): () => void {
  let stopped = false;
  let socket: WebSocket | null = null;
  let retry: ReturnType<typeof setTimeout> | undefined;

  const open = () => {
    if (stopped) return;
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(`${protocol}//${window.location.host}/api/ws/terminal`);
    socket.onopen = () => handlers.onStatus?.('open');
    socket.onclose = () => {
      handlers.onStatus?.('closed');
      if (!stopped) retry = setTimeout(open, 2500);
    };
    socket.onerror = () => handlers.onStatus?.('error');
    socket.onmessage = (message) => {
      try {
        handlers.onEvent(JSON.parse(message.data) as TerminalEvent);
      } catch {
        // Ignore malformed frames.
      }
    };
  };
  open();
  return () => {
    stopped = true;
    if (retry) clearTimeout(retry);
    socket?.close();
  };
}
