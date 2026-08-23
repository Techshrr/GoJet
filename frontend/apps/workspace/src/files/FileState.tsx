import type { FileRecord, FileScanState } from '@gojet/api-client';

const stateCopy: Record<FileScanState, { icon: string; headline: string; next: string }> = {
  quarantined: { icon: 'PackageLock', headline: 'File quarantined', next: 'Stored privately. Waiting for the mandatory ClamAV worker.' },
  scanning: { icon: 'LoaderCircle', headline: 'Security scan in progress', next: 'Distribution remains denied until a current scan verdict completes.' },
  safe: { icon: 'ShieldCheck', headline: 'File is safe to publish', next: 'The current scan is clean. Publication remains a separate authorized action.' },
  blocked: { icon: 'ShieldX', headline: 'File blocked', next: 'Distribution is denied. Review the scan evidence or remove the file.' },
  scan_error: { icon: 'TriangleAlert', headline: 'Scan unavailable; file remains private', next: 'Restore scanner health, then request a new scan.' },
};

export function FileState({ item, compact = false }: { item: Pick<FileRecord, 'scan_state' | 'published'>; compact?: boolean }) {
  const copy = stateCopy[item.scan_state];
  return (
    <div className={compact ? 'file-state file-state--compact' : 'file-state'} data-state={item.scan_state} role="status">
      <span className="file-state-icon" data-icon={copy.icon} aria-hidden="true">{item.scan_state === 'safe' ? '✓' : item.scan_state === 'blocked' ? '×' : item.scan_state === 'scan_error' ? '!' : item.scan_state === 'scanning' ? '↻' : '▣'}</span>
      <div>
        <strong>{copy.headline}</strong>
        {!compact ? <p>{item.scan_state === 'safe' && item.published ? 'Published through the authorized GoJet download path.' : copy.next}</p> : null}
      </div>
    </div>
  );
}
