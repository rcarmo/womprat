// Local-only address/search suggestions. Persisted inputs come from encrypted
// app settings; no network suggestion queries or plaintext search-history copy.
function cleanHistoryInput(value) {
  if (typeof value !== 'string') return '';
  const text = value.trim();
  if (!text || text.length > 4096 || /[\u0000-\u001f\u007f]/.test(text)) return '';
  if (/^(javascript|data|file|vbscript|about|blob):/i.test(text)) return '';
  return text;
}
export function addressHistorySuggestions(input, values, limit = 4) {
  const query = String(input || '').trim().toLowerCase();
  const unique = [...new Set(values.map(cleanHistoryInput).filter(Boolean))];
  // Prefix matches first, then substring matches, keeping recency within each.
  const prefix = [], rest = [];
  for (const text of unique) {
    const match = text.toLowerCase();
    const address = match.replace(/^(https?|ssh|vnc|rdp):\/\//, '');
    if (!query || match.startsWith(query) || address.startsWith(query)) prefix.push(text);
    else if (match.includes(query)) rest.push(text);
  }
  return prefix.concat(rest).slice(0, limit);
}
