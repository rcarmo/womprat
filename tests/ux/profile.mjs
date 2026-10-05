import { writeFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';

export const buildDir = process.env.WOMPRAT_BUILD_DIR;
export const runDir = process.env.WOMPRAT_RUN_DIR;
export const profileDir = process.env.WOMPRAT_PROFILE_DIR;
if (!buildDir || !runDir || !profileDir) {
  throw new Error('Use make ux-test or scripts/bun-profile.sh; project paths/profiling are required');
}

// Capture the Chromium renderer as well as the Bun driver.
export async function profilePage(page, name) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Profiler.enable');
  await cdp.send('Profiler.setSamplingInterval', { interval: 1000 });
  await cdp.send('Profiler.start');
  await cdp.send('HeapProfiler.startSampling', { samplingInterval: 4096 });
  return async () => {
    const { profile: cpu } = await cdp.send('Profiler.stop');
    const { profile: heap } = await cdp.send('HeapProfiler.stopSampling');
    mkdirSync(profileDir, { recursive: true });
    writeFileSync(join(profileDir, `${name}.cpuprofile`), JSON.stringify(cpu));
    writeFileSync(join(profileDir, `${name}.heapprofile`), JSON.stringify(heap));
    await cdp.detach();
  };
}
