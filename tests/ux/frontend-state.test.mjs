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

test('post-auth hydration loads appearance before tabs exactly once', async () => {
  const begin = source.indexOf('let shellHydrated = false;');
  const end = source.indexOf('async function checkAuth()', begin);
  const events = [];
  const hydrate = Function('loadTerminalAppearance','hydrateNativeTabs',`${source.slice(begin,end)}; return hydrateShellAfterAuth`)(
    async () => events.push('appearance'), async () => events.push('tabs'));
  await Promise.all([hydrate(), hydrate()]);
  expect(events).toEqual(['appearance','tabs']);
});
