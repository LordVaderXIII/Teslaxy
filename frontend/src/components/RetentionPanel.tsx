import { useRef, useState } from 'react';
import { X } from 'lucide-react';
import { getRetentionSettings, getRetentionStorage, saveRetentionSettings, type RetentionSettings, type RetentionStorage } from '../utils/retentionApi.mjs';

const timestamp = (value?: string | null) => value ? new Date(value).toLocaleString() : 'Not scheduled';
const gib = (bytes: number) => `${(bytes / 1024 ** 3).toFixed(2)} GiB`;

export default function RetentionPanel() {
  const dialog = useRef<HTMLDialogElement>(null);
  const [settings, setSettings] = useState<RetentionSettings | null>(null);
  const [storage, setStorage] = useState<RetentionStorage | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [storageError, setStorageError] = useState('');
  const [message, setMessage] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    setMessage('');
    setStorageError('');
    const [configResult, storageResult] = await Promise.allSettled([getRetentionSettings(), getRetentionStorage()]);
    if (configResult.status === 'fulfilled') setSettings(configResult.value);
    else {
      setSettings(null);
      setError('Could not load retention settings. Check access and retry.');
    }
    if (storageResult.status === 'fulfilled') setStorage(storageResult.value);
    else {
      setStorage(null);
      setStorageError('Could not load storage. Check access and retry.');
    }
    setLoading(false);
  };

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!settings || saving) return;
    setSaving(true);
    setMessage('');
    setError('');
    try {
      const saved = await saveRetentionSettings(settings);
      setSettings({ ...saved, status: settings.status });
      setMessage('Settings saved.');
      // PUT returns config only. Refresh history/next-run without round-tripping status in PUT.
      try { setSettings(await getRetentionSettings()); }
      catch { setError('Settings saved, but run status could not be refreshed. Reopen to retry.'); }
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : 'Settings could not be saved. Please retry.');
    } finally {
      setSaving(false);
    }
  };

  const status = settings?.status;
  return <>
    <button type="button" className="desk-lightbtn" onClick={() => { dialog.current?.showModal(); void load(); }}>
      Retention &amp; storage
    </button>
    <dialog ref={dialog} aria-labelledby="retention-title"
      className="m-auto w-[min(760px,calc(100%-24px))] max-h-[90dvh] overflow-y-auto bg-[var(--panel)] text-[var(--ink-2)] border border-[var(--line)] rounded-[5px] p-0 backdrop:bg-black/70">
      <div className="p-5 border-b border-dashed border-[var(--line)] flex justify-between items-start gap-3">
        <div>
          <span className="desk-eyebrow">// FOOTAGE POLICY</span>
          <h2 id="retention-title" className="desk-title text-[28px] mt-2">Retention &amp; storage</h2>
        </div>
        <button type="button" className="desk-iconbtn" aria-label="Close retention settings" onClick={() => dialog.current?.close()}><X size={20} /></button>
      </div>
      <div className="p-5 space-y-5">
        <p className="text-[var(--muted)]">Saved clips and protected events are never pruned. Changes apply only when you save. Dry-run reports candidates without deleting footage.</p>
        {loading && <p role="status">Loading retention &amp; storage…</p>}
        {error && <p role="alert" className="text-[var(--sev-crit)]">{error}</p>}
        {settings && <form onSubmit={save}>
          <fieldset disabled={loading || saving} className="border-0 p-0 m-0 space-y-4">
            <div className="flex flex-wrap gap-x-8 gap-y-2">
              <label className="flex items-center gap-3 min-h-11"><input type="checkbox" className="h-5 w-5 accent-[var(--accent)]" checked={settings.enabled} onChange={e => setSettings({ ...settings, enabled: e.target.checked })} />Enable retention</label>
              <label className="flex items-center gap-3 min-h-11"><input type="checkbox" className="h-5 w-5 accent-[var(--accent)]" checked={settings.dry_run} onChange={e => setSettings({ ...settings, dry_run: e.target.checked })} />Dry-run only</label>
            </div>
            <div className="grid grid-cols-2 max-sm:grid-cols-1 gap-4">
              <label className="flex flex-col gap-2">Max age (days)
                <input type="number" required min="0" max="36500" step="1" value={Number.isNaN(settings.max_age_days) ? '' : settings.max_age_days} onChange={e => setSettings({ ...settings, max_age_days: e.target.valueAsNumber })} className="min-h-11 min-w-0 w-full p-2 bg-[var(--bg)] text-[var(--ink)] border border-[var(--line)] rounded-[3px]" />
              </label>
              <label className="flex flex-col gap-2">Max size (GB)
                <input type="number" required min="0" max="1048576" step="any" value={Number.isNaN(settings.max_size_gb) ? '' : settings.max_size_gb} onChange={e => setSettings({ ...settings, max_size_gb: e.target.valueAsNumber })} className="min-h-11 min-w-0 w-full p-2 bg-[var(--bg)] text-[var(--ink)] border border-[var(--line)] rounded-[3px]" />
              </label>
            </div>
            <p className="text-[var(--muted)]">0 disables that limit. Runs every {settings.interval_hours} hours; files younger than {settings.min_file_age_minutes} minutes are skipped.</p>
            <button type="submit" className="desk-primary">{saving ? 'Saving…' : 'Save settings'}</button>
          </fieldset>
        </form>}
        {message && <p role="status" className="text-[var(--accent)]">{message}</p>}
        {settings && <section aria-label="Retention run history" className="border-t border-dashed border-[var(--line)] pt-4 space-y-2">
          <h3 className="desk-eyebrow">// RUN HISTORY</h3>
          <p>Last run: {status?.last_run ? `${timestamp(status.last_run.finished_at)} · ${status.last_run.dry_run ? 'dry-run' : 'prune'} · ${status.last_run.deleted_count} deleted · ${status.last_run.error_count} errors` : 'Never run'}</p>
          <p>Next run (estimate): {timestamp(status?.next_run)}</p>
          <p>Last dry-run: {status?.last_dry_run ? `${timestamp(status.last_dry_run.finished_at)} · would delete ${status.last_dry_run.candidate_count} ${status.last_dry_run.candidate_count === 1 ? 'event' : 'events'} (${status.last_dry_run.candidate_gib.toFixed(3)} GiB) · ${status.last_dry_run.error_count} errors` : 'No dry-run recorded'}</p>
        </section>}
        <section aria-label="Footage storage" className="border-t border-dashed border-[var(--line)] pt-4 space-y-3">
          <h3 className="desk-eyebrow">// STORAGE</h3>
          {storageError && <p role="alert" className="text-[var(--sev-crit)]">{storageError}</p>}
          {storage && <>
            <dl className="grid grid-cols-3 max-sm:grid-cols-1 gap-3">
              {(['Sentry', 'Recent', 'Saved'] as const).map(type => <div key={type} className="p-3 border border-[var(--line)] bg-[var(--bg)]"><dt className="text-[var(--muted)]">{type}</dt><dd className="text-[var(--ink)] mt-1">{gib(storage.by_type_bytes[type] || 0)}</dd></div>)}
            </dl>
            <p>Volume used: {gib(storage.volume_used_bytes)} · free: {gib(storage.volume_free_bytes)} · total: {gib(storage.volume_total_bytes)}</p>
            <p className="text-[var(--muted)]">Footage totals are logical file bytes. Volume totals include other files on the same filesystem.</p>
          </>}
        </section>
        <button type="button" className="desk-lightbtn" disabled={loading || saving} onClick={() => void load()}>Reload retention &amp; storage</button>
      </div>
    </dialog>
  </>;
}
