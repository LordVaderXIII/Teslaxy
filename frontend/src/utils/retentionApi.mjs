// Read a locally supplied session token only; never add credentials to URLs.
function sessionToken() {
  return globalThis.sessionStorage?.getItem('token') || '';
}

async function request(path, method = 'GET', body, token = sessionToken()) {
  const headers = { Accept: 'application/json' };
  if (token) headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    headers['X-Requested-With'] = 'XMLHttpRequest';
  }
  const response = await fetch(path, {
    method, mode: 'same-origin', credentials: 'same-origin', headers,
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  if (!response.ok) throw new Error(`Request failed (${response.status}). Check access and try again.`);
  return response.json();
}

export function getRetentionSettings(token) {
  return request('/api/retention/settings', 'GET', undefined, token);
}

export function getRetentionStorage(token) {
  return request('/api/retention/storage', 'GET', undefined, token);
}

export function saveRetentionSettings(settings, token) {
  // PUT replaces the config. Preserve scheduler fields, never send footage_path/status.
  const { enabled, dry_run, max_age_days, max_size_gb, interval_hours, min_file_age_minutes } = settings;
  return request('/api/retention/settings', 'PUT', {
    enabled, dry_run, max_age_days, max_size_gb, interval_hours, min_file_age_minutes,
  }, token);
}

export function setClipProtected(id, protectedValue, token, ids) {
  return request(`/api/clips/${id}/protect`, 'POST', {
    protected: protectedValue, ...(ids === undefined ? {} : { ids }),
  }, token);
}
