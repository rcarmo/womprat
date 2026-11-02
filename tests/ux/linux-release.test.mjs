import { test, expect } from 'bun:test';
import { readFileSync, mkdtempSync, mkdirSync, copyFileSync, writeFileSync, rmSync, symlinkSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';

const release = readFileSync('.github/workflows/release.yml', 'utf8');
const linux = readFileSync('.github/workflows/linux-appimage.yml', 'utf8');
const script = readFileSync('scripts/linux-release.sh', 'utf8');
const requiredHooks = [
  'scripts/linux-appimage-build.sh', 'scripts/appimage-apprun.sh',
  'tests/linux/browser-parity.mjs', 'tests/linux/fedora-appimage.sh',
  'tests/linux/appimage-layout.sh', 'tests/linux/fedora-native-run.sh',
  'tests/linux/session-restart.mjs', 'tests/linux/release-smoke.mjs',
  'tests/linux/close-window.c',
];

test('Linux release preflight finds checked-in hooks but does not perform acceptance', () => {
  const result = spawnSync('bash', ['scripts/linux-release.sh', '--check'], { encoding: 'utf8' });
  expect(result.status).toBe(0);
  expect(result.stdout).toBe('');
});

test('hook symlinks and missing hooks cannot enable preflight', () => {
  const root = mkdtempSync(join(process.env.WOMPRAT_RUN_DIR, 'linux-gate-'));
  try {
    mkdirSync(join(root, 'scripts')); mkdirSync(join(root, 'tests/linux'), { recursive: true });
    copyFileSync('scripts/linux-release.sh', join(root, 'scripts/linux-release.sh'));
    writeFileSync(join(root, 'scripts/paths.sh'), 'WOMPRAT_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"\n');
    for (const hook of requiredHooks) writeFileSync(join(root, hook), '# fixture\n');
    const invoke = () => spawnSync('bash', [join(root, 'scripts/linux-release.sh'), '--check'], { encoding: 'utf8' });
    expect(invoke().status).toBe(0); // Presence only; acceptance is separate.
    for (const hook of requiredHooks) {
      rmSync(join(root, hook));
      symlinkSync(join(root, 'scripts/paths.sh'), join(root, hook));
      expect(invoke().status).toBe(1);
      rmSync(join(root, hook));
      expect(invoke().status).toBe(1);
      writeFileSync(join(root, hook), '# fixture\n');
    }
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test('a failed browser or Fedora acceptance cannot stage release assets', () => {
  const root = mkdtempSync(join(process.env.WOMPRAT_RUN_DIR, 'linux-acceptance-'));
  try {
    mkdirSync(join(root, 'scripts')); mkdirSync(join(root, 'tests/linux'), { recursive: true });
    mkdirSync(join(root, 'bin')); mkdirSync(join(root, 'dist')); mkdirSync(join(root, 'run'));
    copyFileSync('scripts/linux-release.sh', join(root, 'scripts/linux-release.sh'));
    writeFileSync(join(root, 'scripts/paths.sh'), `WOMPRAT_REPO='${root}'\nWOMPRAT_RUN_DIR='${root}/run'\nWOMPRAT_BUILD_DIR='${root}'\n`);
    for (const hook of requiredHooks) writeFileSync(join(root, hook), '# fixture\n');
    writeFileSync(join(root, 'scripts/linux-appimage-build.sh'), 'printf image > "$2"; chmod +x "$2"\n');
    writeFileSync(join(root, 'tests/linux/browser-parity.mjs'), '// invoked through fake Bun to test orchestration only\n');
    writeFileSync(join(root, 'tests/linux/fedora-appimage.sh'), 'exit "${FEDORA_EXIT:-0}"\n');
    writeFileSync(join(root, 'bin/bun'), '#!/usr/bin/env bash\nexit "${PARITY_EXIT:-0}"\n', { mode: 0o755 });
    const invoke = (env) => spawnSync('bash', [join(root, 'scripts/linux-release.sh')], { encoding: 'utf8', env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, VERSION: '0.4.5', COMMIT: '534cd67', ...env } });
    for (const vars of [{ PARITY_EXIT: '7' }, { FEDORA_EXIT: '8' }]) {
      expect(invoke(vars).status).not.toBe(0);
      expect(spawnSync('find', [join(root, 'dist'), '-type', 'f'], { encoding: 'utf8' }).stdout).toBe('');
    }
    expect(invoke({}).status).toBe(0);
    expect(readFileSync(join(root, 'dist/LINUX-SHA256SUMS.txt'), 'utf8')).toContain('Womprat-0.4.5-x86_64.AppImage');
    expect(invoke({ VERSION: '../../bad' }).status).toBe(2);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test('release assets cannot be staged before both acceptance hooks pass', () => {
  const parity = script.indexOf('bun tests/linux/browser-parity.mjs');
  const fedora = script.indexOf('bash tests/linux/fedora-appimage.sh');
  const copy = script.indexOf('cp "$image"');
  expect(parity).toBeGreaterThan(script.indexOf('bash scripts/linux-appimage-build.sh --output'));
  expect(fedora).toBeGreaterThan(parity);
  expect(copy).toBeGreaterThan(fedora);
  expect(script).toContain('--require-release');
  expect(script).toContain('--fedora-version 44');
  expect(script).toContain('set -euo pipefail');
});

test('AppImage helper relocation preserves offsets and fails on unknown WebKit layout', () => {
  const root=mkdtempSync(join(process.env.WOMPRAT_RUN_DIR,'webkit-relocate-'));
  try {
    const old='/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1';
    const file=join(root,'libwebkit.so');
    const original=Buffer.concat([Buffer.from('fixture-prefix'),Buffer.from(old+'\0'),Buffer.from(old+'/injected-bundle/\0'),Buffer.from('fixture-tail')]);
    writeFileSync(file,original);
    const result=spawnSync(process.execPath,['scripts/relocate-webkit.mjs',file],{encoding:'utf8'});
    expect(result.status).toBe(0);
    const relocated=readFileSync(file);
    expect(relocated.length).toBe(original.length);
    expect(relocated.subarray(0,14).toString()).toBe('fixture-prefix');
    expect(relocated.subarray(-12).toString()).toBe('fixture-tail');
    expect(relocated.includes(Buffer.from('/opt/womprat/usr/lib/webkit2gtk-4.1\0'))).toBe(true);
    expect(relocated.includes(Buffer.from('/opt/womprat/usr/lib/webkit2gtk-4.1/injected-bundle/\0'))).toBe(true);
    expect(spawnSync(process.execPath,['scripts/relocate-webkit.mjs',file]).status).not.toBe(0);
    writeFileSync(file,Buffer.concat([original,original]));
    expect(spawnSync(process.execPath,['scripts/relocate-webkit.mjs',file]).status).not.toBe(0);
  } finally {rmSync(root,{recursive:true,force:true})}
  const launcher=readFileSync('scripts/appimage-apprun.sh','utf8');
  expect(launcher).toContain('opt_root=$(readlink -f -- /opt)');
  expect(launcher).toContain('--tmpfs "$opt_root" --ro-bind "$APPDIR" "$opt_root/womprat"');
  expect(launcher).toContain('$APPDIR != "$opt_root/womprat"');
  expect(launcher).toContain('--chdir /opt/womprat /opt/womprat/AppRun');
  expect(launcher).not.toContain('WEBKIT_DISABLE_SANDBOX');
});

test('CI wires main/PR/release parity gates and separate accepted checksums', () => {
  expect(linux).toContain('branches: [main]');
  expect(linux).toContain('pull_request:');
  expect(linux).toContain('workflow_call:');
  expect(linux.indexOf('run: make linux-release-check')).toBeLessThan(linux.indexOf('actions/setup-go'));
  expect(linux).toContain('name: linux-appimage-release');
  expect(release).toContain('uses: ./.github/workflows/linux-appimage.yml');
  expect(release).toContain('needs: [linux-appimage, build-release]');
  expect(release).toContain('sha256sum --check LINUX-SHA256SUMS.txt');
  expect(release).toContain('fail_on_unmatched_files: true');
  expect(linux).not.toContain('continue-on-error');
  expect(readFileSync('Makefile', 'utf8').split('linux-gui:')[1].split('darwin:')[0]).not.toContain('main.debugBuild=1');
});

// Record-only profiling must not start a GUI after the app exits.
test("native profiler is noninteractive and accepts Fedora compression", () => {
 const runner=readFileSync("tests/linux/fedora-native-run.sh","utf8");
 expect(runner).toContain("heaptrack --record-only -o");
 expect(runner).toContain("-name '*.zst'");
 expect(runner).toContain("heaptrack_print -f");
 expect(runner).toContain("--call-graph dwarf,8192");
});

test('CPU collector privilege is not inherited by the GUI', () => {
 const container=readFileSync('tests/linux/fedora-appimage.sh','utf8');
 const native=readFileSync('tests/linux/fedora-native-run.sh','utf8');
 expect(container).toContain('setcap cap_perfmon=ep /usr/bin/perf');
 expect(native).toContain('^CapEff:[[:space:]]+0+$');
 expect(native).toContain('[[ $(id -u) != 0 ]]');
 expect(container).not.toContain('sysctl -w');
});

test('native app links use the existing shell panel routing', () => {
 expect(readFileSync('cmd/womprat/gui_linux.go','utf8')).toContain('window.wompratOpenSpecialURL(%s)');
 expect(readFileSync('cmd/womprat/frontend/index.html','utf8')).toContain('window.wompratOpenSpecialURL = openSpecialURL');
});

test('Fedora Atomic layout regression gates main/PR CI and release publication', () => {
 const fedora=readFileSync('tests/linux/fedora-appimage.sh','utf8');
 const native=readFileSync('tests/linux/fedora-native-run.sh','utf8');
 const layout=readFileSync('tests/linux/appimage-layout.sh','utf8');
 for (const hook of requiredHooks) expect(script).toContain(`  ${hook}\n`);
 const regression=fedora.indexOf('bash "$stage/source/tests/linux/appimage-layout.sh"');
 const atomic=fedora.indexOf("step='Fedora Atomic /opt layout'");
 const acceptance=fedora.indexOf('bash "$stage/source/tests/linux/fedora-native-run.sh"');
 expect(regression).toBeGreaterThan(0);
 expect(atomic).toBeGreaterThan(regression);
 expect(acceptance).toBeGreaterThan(atomic);
 expect(fedora).toContain('ln -s var/opt /opt');
 expect(fedora).toContain('dst=/usr/local/go,readonly');
 expect(layout).toContain('for layout in directory absolute-symlink relative-symlink;do');
 expect(layout).toContain('runuser -u "$user"');
 expect(native).toContain('bun tests/linux/session-restart.mjs "$image"');
 expect(linux).toContain('make linux-appimage VERSION="$version"');
 expect(linux.indexOf('name: Upload accepted Linux release assets')).toBeGreaterThan(linux.indexOf('make linux-appimage VERSION='));
 expect(release).toContain('uses: ./.github/workflows/linux-appimage.yml');
 expect(release).toContain('needs: [linux-appimage, build-release]');
 expect(release.indexOf('sha256sum --check LINUX-SHA256SUMS.txt')).toBeLessThan(release.indexOf('name: Publish accepted Linux assets'));
});
