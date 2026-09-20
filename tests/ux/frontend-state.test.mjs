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

test('native tab registrations preserve call order', async () => {
  const begin = source.indexOf('let nativeTabRegistrationQueue = Promise.resolve();');
  const end = source.indexOf('function loadURLHistory()', begin);
  const events = [];
  const register = Function('window',`${source.slice(begin,end)}; return registerLocalTab`)(
    { womprat_registerLocalTab: async payload => { const tab=JSON.parse(payload); await Bun.sleep(tab.delay); events.push(tab.id); } });
  await Promise.all([register({id:'first',delay:10}), register({id:'second',delay:0})]);
  expect(events).toEqual(['first','second']);
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

test('post-auth hydration loads appearance before tabs exactly once', async () => {
  const begin = source.indexOf('let shellHydrated = false;');
  const end = source.indexOf('async function checkAuth()', begin);
  const events = [];
  const hydrate = Function('loadTerminalAppearance','hydrateNativeTabs',`${source.slice(begin,end)}; return hydrateShellAfterAuth`)(
    async () => events.push('appearance'), async () => events.push('tabs'));
  await Promise.all([hydrate(), hydrate()]);
  expect(events).toEqual(['appearance','tabs']);
});
