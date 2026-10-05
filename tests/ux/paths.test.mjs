import { test, expect } from 'bun:test';
import { mkdtempSync, mkdirSync, symlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';

const root = mkdtempSync(join(process.env.WOMPRAT_RUN_DIR, 'resolver-'));
const base = join(root, 'base');
const runner = join(root, 'runner');
const inherited = join(root, 'original');
const system = join(root, 'system');
for (const path of [base, runner, inherited, system]) mkdirSync(path);
function run(vars = {}, command = 'project_tmp_resolve womprat', workspace = base) {
  const env = { ...process.env };
  for (const key of ['PROJECT_TMP_BASE','PROJECT_TMP_ROOT','PROJECT_ORIGINAL_TMPDIR','WOMPRAT_RUN_DIR','CI','GITHUB_ACTIONS','GITLAB_CI','TF_BUILD','CIRCLECI','RUNNER_TEMP']) delete env[key];
  Object.assign(env, { TMPDIR: inherited, PROJECT_SYSTEM_TEMP: system }, vars);
  const out = spawnSync('bash', ['-c', `source scripts/project-tmp.sh; ${command === 'project_tmp_resolve womprat' ? `${command} "${workspace}"` : command}`], { env, encoding: 'utf8' });
  return { code: out.status, text: out.stdout.trim(), error: out.stderr };
}
test('explicit base, root and agreeing overrides', () => {
  for (const vars of [{ PROJECT_TMP_BASE: base }, { PROJECT_TMP_ROOT: `${base}/womprat` }, { PROJECT_TMP_BASE: base, PROJECT_TMP_ROOT: `${base}/womprat/` }]) {
    expect(run(vars)).toMatchObject({ code: 0, text: `${base}/womprat` });
  }
});
test('empty, relative, traversal, wrong-name and conflicting overrides fail', () => {
  for (const vars of [{PROJECT_TMP_BASE:''},{PROJECT_TMP_BASE:'relative'},{PROJECT_TMP_BASE:`${base}/../other`},{PROJECT_TMP_ROOT:''},{PROJECT_TMP_ROOT:'relative/womprat'},{PROJECT_TMP_ROOT:`${base}/wrong`},{PROJECT_TMP_BASE:base,PROJECT_TMP_ROOT:`${runner}/womprat`}]) expect(run(vars).code).not.toBe(0);
});
test('CI ignores usable workspace; runner, inherited TMPDIR, then platform', () => {
  expect(run({CI:'true',RUNNER_TEMP:runner}).text).toBe(`${runner}/womprat`);
  expect(run({CI:'true',RUNNER_TEMP:'relative'}).text).toBe(`${inherited}/womprat`);
  expect(run({CI:'true',TMPDIR:''}).text).toBe(`${system}/womprat`);
  expect(run({GITHUB_ACTIONS:'true',RUNNER_TEMP:runner}).text).toBe(`${runner}/womprat`);
});
test('local ignores runner and inherited TMPDIR; uses workspace then system', () => {
  expect(run({RUNNER_TEMP:runner}).text).toBe(`${base}/womprat`);
  const file = join(root,'file'); writeFileSync(file,'not a directory');
  expect(run({RUNNER_TEMP:runner}, undefined, file).text).toBe(`${system}/womprat`);
});
test('symlink root and ancestor fail without modifying their targets', () => {
  symlinkSync(base,join(root,'linked'));
  symlinkSync(base,join(runner,'womprat'));
  expect(run({PROJECT_TMP_BASE:join(root,'linked')}).code).not.toBe(0);
  expect(run({PROJECT_TMP_ROOT:join(runner,'womprat')}).code).not.toBe(0);
});
test('paths snapshot and propagate original TMPDIR without nesting', () => {
  const result = run({PROJECT_TMP_BASE:base}, 'source scripts/paths.sh; first=$PROJECT_TMP_ROOT; source scripts/paths.sh; printf "%s|%s|%s" "$first" "$PROJECT_TMP_ROOT" "$PROJECT_ORIGINAL_TMPDIR"');
  expect(result).toMatchObject({code:0,text:`${base}/womprat|${base}/womprat|${inherited}`});
});
test('symlink scratch children fail closed', () => {
  const owned = join(root, 'scratch'); mkdirSync(owned);
  mkdirSync(join(owned, 'womprat')); mkdirSync(join(owned, 'womprat', 'build'));
  symlinkSync(base, join(owned, 'womprat', 'build', 'dist'));
  expect(run({ PROJECT_TMP_BASE: owned }, 'source scripts/paths.sh').code).not.toBe(0);
});
test('make command-line overrides validate before recipes', () => {
  const good = run({}, `make --no-print-directory paths PROJECT_TMP_BASE='${base}'`);
  expect(good.code).toBe(0); expect(good.text).toContain(`WOMPRAT_TMP_ROOT=${base}/womprat`);
  expect(run({}, `make --no-print-directory paths PROJECT_TMP_BASE=relative`).code).not.toBe(0);
  const confined = run({}, `make --no-print-directory paths PROJECT_TMP_BASE='${base}' WOMPRAT_TMP_ROOT=/bad TMP_DIR=/bad DIST_DIR=/bad`);
  expect(confined.code).toBe(0); expect(confined.text).toContain(`WOMPRAT_TMP_ROOT=${base}/womprat`);
});
