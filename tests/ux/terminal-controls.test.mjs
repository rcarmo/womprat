import { test, expect } from 'bun:test';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../../cmd/womprat/frontend/index.html', import.meta.url), 'utf8');
function extractFunction(name, nextName) {
  const begin = source.indexOf(`function ${name}(`);
  const end = source.indexOf(`function ${nextName}(`, begin);
  if (begin < 0 || end < 0) throw new Error(`cannot extract ${name}`);
  return Function(`${source.slice(begin, end)}; return ${name}`)();
}
const handleTerminalCtrlC = extractFunction('handleTerminalCtrlC', 'applyTerminalAppearance');

test('Ctrl+C copies a terminal selection without sending ETX', async () => {
  const writes = [];
  const inputs = [];
  const term = {
    hasSelection: () => true,
    getSelection: () => 'selected text',
    input: (...args) => inputs.push(args),
  };
  const result = handleTerminalCtrlC(term, { writeText: text => writes.push(text) });
  await Promise.resolve();
  expect(result).toBe(false);
  expect(writes).toEqual(['selected text']);
  expect(inputs).toEqual([]);
});

test('Ctrl+C sends ETX when the terminal has no selection', () => {
  const writes = [];
  const inputs = [];
  const term = {
    hasSelection: () => false,
    getSelection: () => '',
    input: (...args) => inputs.push(args),
  };
  const result = handleTerminalCtrlC(term, { writeText: text => writes.push(text) });
  expect(result).toBe(false);
  expect(writes).toEqual([]);
  expect(inputs).toEqual([['\x03', true]]);
});

test('clipboard write rejection is contained', async () => {
  const warnings = [];
  const originalWarn = console.warn;
  console.warn = (...args) => warnings.push(args);
  try {
    const term = { hasSelection: () => true, getSelection: () => 'text', input() {} };
    handleTerminalCtrlC(term, { writeText: () => Promise.reject(new Error('denied')) });
    await Bun.sleep(0);
    expect(warnings.length).toBe(1);
  } finally {
    console.warn = originalWarn;
  }
});
