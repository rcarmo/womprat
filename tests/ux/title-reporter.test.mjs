import { test, expect } from 'bun:test';
import { chromium } from 'playwright';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../../cmd/womprat/native_content_windows.go', import.meta.url), 'utf8');
const script = source.split('const browserTitleReporterJS = `')[1]?.split('`\n\nfunc parseTitleMessage')[0];
if (!script) throw new Error('browser title reporter script missing');

test('native title reporter observes JavaScript title text mutations', async () => {
  const browser = await chromium.launch({headless: true});
  try {
    const page = await browser.newPage();
    await page.goto('about:blank');
    await page.setContent('<head><title>Initial</title></head><body>page</body>');
    await page.evaluate(() => {
      window.__titles = [];
      window.chrome = {webview: {postMessage: raw => window.__titles.push(JSON.parse(raw).wompratTitle)}};
    });
    await page.evaluate(script);
    await page.evaluate(() => {
      window.__titles = [];
      document.querySelector('title').firstChild.data = 'Changed by JavaScript';
    });
    await page.waitForFunction(() => window.__titles.includes('Changed by JavaScript'));
    expect(await page.title()).toBe('Changed by JavaScript');
    expect(await page.evaluate(() => window.__titles)).toContain('Changed by JavaScript');
    await page.evaluate(() => {
      window.__titles = [];
      document.title = 'Assigned by JavaScript';
    });
    await page.waitForFunction(() => window.__titles.includes('Assigned by JavaScript'));
    expect(await page.title()).toBe('Assigned by JavaScript');
  } finally {
    await browser.close();
  }
});
