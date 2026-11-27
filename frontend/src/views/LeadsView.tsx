import { useEffect, useMemo, useState } from 'react';
import { Download, FileJson, Search, SlidersHorizontal } from 'lucide-react';
import { api, type Lead } from '../api/client';
import { EmptyState, ErrorState, Panel, SectionTitle, Skeletons } from '../components/ui';
import { useI18n } from '../i18n';
import LeadCard from './LeadCard';

export default function LeadsView() {
  const { t } = useI18n();
  const [leads, setLeads] = useState<Lead[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const [debouncedQuery, setDebouncedQuery] = useState('');
  const [category, setCategory] = useState('all');
  const [status, setStatus] = useState('all');
  const [reload, setReload] = useState(0);
  const [offset, setOffset] = useState(0);
  const limit = 25;

  const statuses = useMemo(() => ['all', 'new', 'in_progress', 'replied', 'closed', 'false_positive', 'hidden'], []);
  const categories = useMemo(() => ['all', 'clear', 'potential', 'doubtful'], []);

  useEffect(() => {
    const handle = setTimeout(() => setDebouncedQuery(query.trim()), 280);
    return () => clearTimeout(handle);
  }, [query]);

  useEffect(() => { setOffset(0); }, [debouncedQuery, category, status]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');
    api.listLeads({
      query: debouncedQuery || undefined,
      category: category === 'all' ? undefined : category,
      status: status === 'all' ? undefined : status,
      limit,
      offset,
    })
      .then((result) => {
        if (cancelled) return;
        setLeads((current) => {
          if (offset === 0) return result.leads;
          const seen = new Set(current.map((lead) => lead.id));
          return [...current, ...result.leads.filter((lead) => !seen.has(lead.id))];
        });
        setTotal(result.total);
      })
      .catch((err) => { if (!cancelled) setError(err instanceof Error ? err.message : String(err)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [debouncedQuery, category, status, offset, reload]);

  return (
    <div>
      <SectionTitle
        eyebrow="Tier-0 → LLM"
        title={t('leads')}
        subtitle={t('leadsSubtitle')}
        actions={(
          <>
            <a className="btn" href={api.exportLeadsURL('csv')} download><Download className="h-4 w-4" /> CSV</a>
            <a className="btn" href={api.exportLeadsURL('json')} download><FileJson className="h-4 w-4" /> JSON</a>
          </>
        )}
      />

      <Panel className="mb-4 p-3">
        <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
          <label className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-slate-500" />
            <input className="field pl-9" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t('search')} />
          </label>
          <div className="flex flex-wrap items-center gap-2">
            <SlidersHorizontal className="hidden h-4 w-4 text-slate-500 sm:block" />
            <select className="field max-w-[10rem]" value={category} onChange={(event) => setCategory(event.target.value)}>
              {categories.map((item) => <option key={item} value={item}>{item === 'all' ? t('all') : item}</option>)}
            </select>
            <select className="field max-w-[11rem]" value={status} onChange={(event) => setStatus(event.target.value)}>
              {statuses.map((item) => <option key={item} value={item}>{item === 'all' ? t('all') : item.replace('_', ' ')}</option>)}
            </select>
            <span className="pill">{total} {t('leads')}</span>
          </div>
        </div>
      </Panel>

      {loading && leads.length === 0 ? <Skeletons rows={4} /> : error ? (
        <ErrorState error={error} onRetry={() => setReload((value) => value + 1)} />
      ) : leads.length === 0 ? (
        <Panel><EmptyState text={t('empty')} /></Panel>
      ) : (
        <div className="space-y-3">
          {leads.map((lead) => <LeadCard key={lead.id} lead={lead} onChanged={() => { setOffset(0); setReload((value) => value + 1); }} />)}
          {leads.length < total && (
            <button className="btn w-full" disabled={loading} onClick={() => setOffset((value) => value + limit)}>
              {loading ? t('loading') : `${t('all')} (${total - leads.length})`}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
