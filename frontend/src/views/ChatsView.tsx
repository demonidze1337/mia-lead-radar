import { useEffect, useState } from 'react';
import { Link2, RefreshCw, ShieldAlert, Trash2, Upload } from 'lucide-react';
import { api, type Chat, type TelegramAccount } from '../api/client';
import { Badge, EmptyState, ErrorState, Panel, SectionTitle, Skeletons } from '../components/ui';
import { useI18n } from '../i18n';

export default function ChatsView() {
  const { t } = useI18n();
  const [chats, setChats] = useState<Chat[]>([]);
  const [accounts, setAccounts] = useState<TelegramAccount[]>([]);
  const [links, setLinks] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const [chatList, accountList] = await Promise.all([api.listChats(), api.listAccounts()]);
      setChats(chatList);
      setAccounts(accountList);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const importDialogs = async () => {
    setBusy(true);
    try { await api.importDialogs(); await load(); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  const enqueue = async () => {
    const values = links.split(/\s+/).map((item) => item.trim()).filter(Boolean);
    if (values.length === 0) return;
    setBusy(true);
    try { await api.enqueueLinks(values); setLinks(''); await load(); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  const clearFlood = async (id: number) => {
    setBusy(true);
    try { await api.clearFlood(id); await load(); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  const remove = async (id: number) => {
    if (!window.confirm('Delete this account and its session reference?')) return;
    setBusy(true);
    try { await api.deleteAccount(id); await load(); } catch (err) { setError(err instanceof Error ? err.message : String(err)); } finally { setBusy(false); }
  };

  if (loading) return <Skeletons rows={4} />;
  if (error && chats.length === 0 && accounts.length === 0) return <ErrorState error={error} onRetry={() => void load()} />;

  return (
    <div>
      <SectionTitle
        eyebrow="MTProto"
        title={t('chats')}
        subtitle={t('chatsSubtitle')}
        actions={<button className="btn" onClick={() => void load()}><RefreshCw className="h-4 w-4" /> {t('refresh')}</button>}
      />
      {error && <p className="mb-3 rounded-xl border border-rose-400/20 bg-rose-400/10 p-3 text-sm text-rose-200">{error}</p>}

      <div className="grid gap-4 xl:grid-cols-2">
        <Panel className="p-5">
          <div className="mb-3 flex items-center gap-2"><ShieldAlert className="h-4 w-4 text-amber-200" /><h3 className="text-sm font-semibold">{t('accounts')}</h3></div>
          {accounts.length === 0 ? <EmptyState text={t('empty')} /> : (
            <div className="space-y-2">
              {accounts.map((account) => {
                const flooded = account.flood_until && new Date(account.flood_until).getTime() > Date.now();
                return (
                  <div key={account.id} className="rounded-xl border border-white/[0.06] bg-white/[0.02] p-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-slate-200">{account.label || account.first_name || account.username || `#${account.id}`}</span>
                      <Badge tone={account.status === 'active' ? 'mint' : account.status === 'flood_wait' ? 'amber' : 'rose'}>{account.status}</Badge>
                      {flooded && <Badge tone="amber">Flood · {formatRemaining(account.flood_until!)}</Badge>}
                      <span className="ml-auto text-[11px] text-slate-500">{account.joined_chats_count} chats</span>
                    </div>
                    <p className="mt-1 text-xs text-slate-500">{account.phone_masked || '—'} · daily joins {account.daily_joins_count}</p>
                    {account.last_error && <p className="mt-1 truncate text-[11px] text-rose-300">{account.last_error}</p>}
                    <div className="mt-2 flex gap-2">
                      <button className="btn h-8 px-2 text-xs" disabled={busy} onClick={() => void clearFlood(account.id)}>{t('clearFlood')}</button>
                      <button className="btn-danger h-8 px-2 text-xs" disabled={busy} onClick={() => void remove(account.id)}><Trash2 className="h-3.5 w-3.5" /> {t('delete')}</button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Panel>

        <Panel className="p-5">
          <div className="mb-3 flex items-center gap-2"><Link2 className="h-4 w-4 text-cyan" /><h3 className="text-sm font-semibold">{t('chats')}</h3></div>
          <div className="mb-3 flex gap-2">
            <button className="btn flex-1" disabled={busy} onClick={() => void importDialogs()}><Upload className="h-4 w-4" /> {t('importChats')}</button>
          </div>
          <label><span className="label">t.me links</span><textarea className="field min-h-20" value={links} onChange={(event) => setLinks(event.target.value)} placeholder={'https://t.me/chat\nhttps://t.me/+invite'} /></label>
          <button className="btn mt-2 w-full" disabled={busy || !links.trim()} onClick={() => void enqueue()}>{t('enqueue')}</button>
          <div className="mt-4 max-h-[45vh] space-y-2 overflow-y-auto pr-1">
            {chats.length === 0 ? <EmptyState text={t('empty')} /> : chats.map((chat) => (
              <div key={chat.id} className="rounded-xl border border-white/[0.05] bg-white/[0.02] p-2.5">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="font-medium text-slate-200">{chat.title || chat.username || `#${chat.id}`}</span>
                  <Badge tone={chat.status === 'joined' ? 'mint' : chat.status === 'bad' || chat.status === 'banned' ? 'rose' : 'amber'}>{chat.status}</Badge>
                  <span className="ml-auto text-[11px] text-slate-500">{formatCount(chat.member_count)} members</span>
                </div>
                <p className="mt-1 text-[11px] text-slate-500">{chat.username ? `@${chat.username}` : chat.chat_type} · {chat.msg_velocity_per_hour.toFixed(1)} msg/h · last #{chat.last_message_id}</p>
                {chat.error_reason && <p className="truncate text-[11px] text-rose-300">{chat.error_reason}</p>}
              </div>
            ))}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function formatRemaining(value: string): string {
  const ms = new Date(value).getTime() - Date.now();
  if (ms <= 0) return '0s';
  const minutes = Math.floor(ms / 60000);
  const seconds = Math.floor((ms % 60000) / 1000);
  return minutes > 0 ? `${minutes}m ${seconds}s` : `${seconds}s`;
}

function formatCount(value: number): string {
  return new Intl.NumberFormat().format(value || 0);
}
