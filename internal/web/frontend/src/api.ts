export async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal, cache: 'no-store' });
  if (!response.ok) throw new Error(`Local API returned HTTP ${response.status}.`);
  return response.json();
}
export async function writeJSON<T>(path: string, method: 'PUT' | 'POST', token: string, body: unknown): Promise<T> {
  const response = await fetch(path, { method, headers: { 'Content-Type': 'application/json', 'X-Ping-Token': token }, body: JSON.stringify(body), cache: 'no-store' });
  if (!response.ok) { const detail = (await response.text()).slice(0, 400); throw new Error(detail || `Local API returned HTTP ${response.status}.`); }
  return response.json();
}
