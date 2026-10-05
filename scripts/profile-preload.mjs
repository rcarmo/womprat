import { profile } from 'bun:jsc';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
const dir = process.env.WOMPRAT_PROFILE_DIR;
if (!dir) throw new Error('WOMPRAT_PROFILE_DIR required');
let finish;
const capture = profile(() => new Promise(resolve => { finish = resolve; }), 1000);
let stopped = false;
async function stop() {
  if (stopped) return;
  stopped = true;
  finish();
  const cpu = await capture;
  writeFileSync(join(dir, 'bun-cpu.json'), JSON.stringify(cpu));
  writeFileSync(join(dir, 'bun.heapsnapshot'), Bun.generateHeapSnapshot('v8'));
}
if (process.env.WOMPRAT_PROFILE_TEST === '1') {
  const { afterAll } = await import('bun:test');
  afterAll(stop);
} else {
  process.once('beforeExit', stop);
}
