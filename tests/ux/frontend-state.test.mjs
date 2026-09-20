import { test, expect } from 'bun:test';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../../cmd/womprat/frontend/index.html', import.meta.url), 'utf8');
function extract(startMarker, endMarker, returns) {
  const begin = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, begin);
  if (begin < 0 || end < 0) throw new Error(`cannot extract ${startMarker}`);
  return Function(`${source.slice(begin, end)}; return ${returns}`)();
}

const tabHelpers = extract('function tabDropBeforeID(', 'function updateRemoteTabTitle(', '{ tabDropBeforeID, reorderTab }');

test('tab drop before/after and append preserve stable ID order', () => {
  globalThis.state = { tabs: [{id:'a'}, {id:'b'}, {id:'c'}] };
  globalThis.validTabID = id => typeof id === 'string' && /^[a-z]+$/.test(id);
  globalThis.renderTabs = () => {};
  globalThis.saveOpenTabs = () => {};
  globalThis.queueNativeTabMutation = operation => Promise.resolve().then(operation);
  globalThis.window = { womprat_reorderTab() {} };
  expect(tabHelpers.tabDropBeforeID('a', 'b', true)).toBe('c');
  tabHelpers.reorderTab('a', '');
  expect(state.tabs.map(t => t.id)).toEqual(['b','c','a']);
  tabHelpers.reorderTab('a', 'b');
  expect(state.tabs.map(t => t.id)).toEqual(['a','b','c']);
  tabHelpers.reorderTab('bad/id', 'b');
  expect(state.tabs.map(t => t.id)).toEqual(['a','b','c']);
});

test('blank-tab navigation registers native state before navigating', async () => {
  const begin = source.indexOf('window.navigateFromBar = async function()');
  const end = source.indexOf('// openSpecialURLPreview', begin);
  const events = [];
  const input = { value: 'example.com' };
  const active = { id:'blank', type:'browser', url:'about:blank' };
  const fn = Function('window','document','performance','clearNavStatus','activeTabObj','openSpecialURLPreview','isBlankBrowserTab','registerLocalTab','openBrowser',`let lastURLBarNavigation={url:'',at:0};${source.slice(begin,end)}; return window.navigateFromBar`)(
    { womprat_navigate: async url => events.push(`navigate:${url}`) },
    { getElementById: () => input }, { now: () => 1000 }, () => {}, () => active, () => false,
    () => true, async () => events.push('register'), () => events.push('open'));
  await fn();
  expect(events).toEqual(['register','navigate:example.com']);
});

test('native tab registrations and mutations preserve call order', async () => {
  const begin = source.indexOf('let nativeTabMutationQueue = Promise.resolve();');
  const end = source.indexOf('function loadURLHistory()', begin);
  const events = [];
  const helpers = Function('window',`${source.slice(begin,end)}; return {registerLocalTab,queueNativeTabMutation}`)(
    { womprat_registerLocalTab: async payload => { const tab=JSON.parse(payload); await Bun.sleep(tab.delay); events.push(`register:${tab.id}`); } });
  const registration=helpers.registerLocalTab({id:'blank',delay:10});
  const close=helpers.queueNativeTabMutation(async()=>events.push('close:blank'),'close failed');
  await Promise.all([registration,close]);
  expect(events).toEqual(['register:blank','close:blank']);
});

test('tab persistence coalesces concurrent saves to newest snapshot', async () => {
  const begin = source.indexOf('let pendingTabSnapshot = null;');
  const end = source.indexOf('// Show/hide home panel', begin);
  const snapshots=[];
  globalThis.state={tabs:[{id:'a'}]};
  globalThis.dedupeRecentTabs=tabs=>tabs.map(t=>({...t}));
  globalThis.fetch=async (_url,options)=>{snapshots.push(JSON.parse(options.body).tabs);await Bun.sleep(5);return {ok:true}};
  const save = Function(`${source.slice(begin,end)}; return saveOpenTabs`)();
  const first=save();
  state.tabs=[{id:'b'}]; save();
  state.tabs=[{id:'c'}]; save();
  await first;
  expect(snapshots).toEqual([[{id:'a'}],[{id:'c'}]]);
});

test('terminal registration precedes native activation', () => {
  const start=source.indexOf('window.openTerminal = async function');
  const end=source.indexOf('const existingSession = terminalSessions.get',start);
  const body=source.slice(start,end);
  const register=body.indexOf('await registerLocalTab(tab)');
  const activate=body.indexOf('activateTab(tabId');
  expect(register).toBeGreaterThan(-1);
  expect(activate).toBeGreaterThan(register);
});

test('terminal appearance updates existing sessions with normalized values', () => {
  const begin=source.indexOf('const DEFAULT_TERMINAL_FONT_SIZE');
  const end=source.indexOf('async function loadTerminalAppearance()',begin);
  const fitCalls=[];
  const session={fontSize:0,term:{options:{}},fit:{fit:()=>fitCalls.push(true)}};
  const apply=Function('terminalSessions','requestAnimationFrame',`${source.slice(begin,end)};return applyTerminalAppearance`)(new Map([['tab',session]]),fn=>fn());
  apply({fontSize:18,terminalFont:'consolas'});
  expect(session.fontSize).toBe(18);
  expect(session.term.options.fontSize).toBe(18);
  expect(session.term.options.fontFamily).toContain('Consolas');
  expect(fitCalls.length).toBe(1);
  apply({fontSize:99,terminalFont:'unknown'});
  expect(session.fontSize).toBe(14);
  expect(session.term.options.fontFamily).toContain('FiraCode');
});

test('post-auth hydration loads appearance before tabs exactly once', async () => {
  const begin = source.indexOf('let shellHydrated = false;');
  const end = source.indexOf('async function checkAuth()', begin);
  const events = [];
  const hydrate = Function('loadTerminalAppearance','loadRecentTabs','hydrateNativeTabs',`${source.slice(begin,end)}; return hydrateShellAfterAuth`)(
    async () => events.push('appearance'), async () => events.push('recent'), async () => events.push('tabs'));
  await Promise.all([hydrate(), hydrate()]);
  expect(events).toEqual(['appearance','recent','tabs']);
});
