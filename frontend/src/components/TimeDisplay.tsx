/** Format an ISO-8601 string to a short local datetime. Returns '—' for null/undefined. */
export function formatTime(iso: string | null | undefined): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleString();
}
