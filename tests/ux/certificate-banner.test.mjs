import { test, expect } from 'bun:test';
const html = await Bun.file(new URL('../../cmd/womprat/frontend/index.html', import.meta.url)).text();
const code = html.slice(html.indexOf('const certificatePrompts ='), html.indexOf('async function preflightConnectivity'));
function fixture(native = true) {
  const elements = Object.fromEntries(['nav-status', 'nav-status-text', 'nav-status-proceed'].map(id => [id, { hidden: true, disabled: false, title: '', classList: { remove() {}, toggle() {} } }]));
  const state = { activeTab: 'a' }, calls = [];
  const window = native ? { womprat_proceedCertificate: async (...args) => { calls.push(args); return true; } } : {};
  const result = new Function('window','state','document','wompratReportChrome', code + '\nreturn {applyNavStatus, proceedCertificate, clearNavStatus, restoreCertificateBanner};')(window,state,{getElementById:id=>elements[id]},()=>{});
  return { ...result, window, state, calls, button: elements['nav-status-proceed'] };
}
test('generic load failure never offers Proceed anyway', () => {
  const f=fixture(); f.applyNavStatus('error','Page failed to load.','a'); expect(f.button.hidden).toBe(true);
});
test('native certificate prompt shows consent only for active failed tab', async () => {
  const f=fixture(); f.applyNavStatus('error','Certificate rejected','a');
  f.window.wompratCertificatePrompt('a','opaque-token','https://flint.local:8443/');
  expect(f.button.hidden).toBe(false); expect(f.button.title).toContain('flint.local:8443'); expect(f.button.title).toContain('session');
  await f.proceedCertificate(); expect(f.calls).toEqual([['a','opaque-token']]); expect(f.button.hidden).toBe(true);
  await f.proceedCertificate(); expect(f.calls.length).toBe(1);
});
test('background tabs, loading, cleared prompts and unsupported runtimes cannot proceed', async () => {
  for (const mode of ['background','loading','cleared','unsupported']) {
    const f=fixture(mode!=='unsupported'); f.applyNavStatus('error','TLS error','a');
    f.window.wompratCertificatePrompt(mode==='background'?'b':'a','token','https://flint.local:8443/');
    if (mode==='loading') f.applyNavStatus('loading','Loading','a');
    if (mode==='cleared') f.window.wompratCertificatePrompt('a','','');
    expect(f.button.hidden).toBe(true); await f.proceedCertificate(); expect(f.calls).toHaveLength(0);
  }
});
test('background certificate prompt is restored when its tab becomes active', async () => {
  const f=fixture(); f.window.wompratCertificatePrompt('b','background-token','https://flint.local:8443/');
  expect(f.button.hidden).toBe(true);
  f.state.activeTab='b'; f.restoreCertificateBanner('b');
  expect(f.button.hidden).toBe(false); await f.proceedCertificate();
  expect(f.calls).toEqual([['b','background-token']]);
});
test('expired consent reports an error without keeping the exception button', async () => {
  const f=fixture(); f.window.womprat_proceedCertificate=async()=>false;
  f.applyNavStatus('error','TLS error','a'); f.window.wompratCertificatePrompt('a','token','https://flint.local:8443/');
  await f.proceedCertificate(); expect(f.button.hidden).toBe(true);
});
