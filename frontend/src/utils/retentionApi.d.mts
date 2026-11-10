export interface RetentionRun {
  finished_at: string;
  candidate_count: number;
  candidate_gib: number;
  deleted_count: number;
  error_count: number;
  dry_run: boolean;
}
export interface RetentionSettings {
  enabled: boolean;
  dry_run: boolean;
  max_age_days: number;
  max_size_gb: number;
  interval_hours: number;
  min_file_age_minutes: number;
  status?: { last_run: RetentionRun | null; next_run: string | null; last_dry_run: RetentionRun | null };
}
export interface RetentionStorage {
  by_type_bytes: { Sentry: number; Recent: number; Saved: number };
  volume_total_bytes: number;
  volume_used_bytes: number;
  volume_free_bytes: number;
}
export function getRetentionSettings(token?: string): Promise<RetentionSettings>;
export function getRetentionStorage(token?: string): Promise<RetentionStorage>;
export function saveRetentionSettings(settings: RetentionSettings, token?: string): Promise<RetentionSettings>;
export function setClipProtected(id: number, protectedValue: boolean, token?: string): Promise<{ id: number; protected: boolean }>;
