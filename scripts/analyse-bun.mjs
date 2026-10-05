// Read-only summaries; human interpretation is still required.
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
for (const dir of process.argv.slice(2)) {
  const cpuPath = join(dir, 'bun-cpu.json');
  const cpu = JSON.parse(readFileSync(cpuPath, 'utf8'));
  const heap = JSON.parse(readFileSync(join(dir, 'bun.heapsnapshot'), 'utf8'));
  const fields = heap.snapshot.meta.node_fields;
  const width = fields.length, name = fields.indexOf('name'), size = fields.indexOf('self_size');
  const totals = new Map();
  for (let i = 0; i < heap.nodes.length; i += width) {
    const key = heap.strings[heap.nodes[i + name]];
    const group = totals.get(key) || { bytes: 0, count: 0 };
    group.bytes += heap.nodes[i + size]; group.count++;
    totals.set(key, group);
  }
  const rows = [...totals].sort((a,b) => b[1].bytes - a[1].bytes).slice(0,18);
  const text = `${cpu.functions}\nHeap retained self-size by name (not cumulative allocations):\n${rows.map(([name,g])=>`${g.bytes} bytes / ${g.count} objects: ${name}`).join('\n')}\n`;
  writeFileSync(join(dir, 'bun-top.txt'), text);
  console.log(dir + '\n' + text);
}
