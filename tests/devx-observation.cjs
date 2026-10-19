// Observe the same fixed global sentinels as the Python native fixtures.
const fs = require('node:fs');
const names = [
  'kernel/panic', 'kernel/panic_on_oops', 'kernel/sysrq', 'kernel/modules_disabled',
  'kernel/perf_event_paranoid', 'kernel/kptr_restrict', 'kernel/dmesg_restrict',
  'vm/overcommit_memory', 'vm/swappiness', 'vm/max_map_count', 'vm/nr_hugepages',
  'fs/protected_hardlinks', 'fs/protected_symlinks', 'fs/protected_fifos',
  'fs/protected_regular', 'user/max_user_namespaces',
];
const phase = process.argv[2];
if (!['before', 'after'].includes(phase)) throw new Error('requires a before or after observation');
const values = Object.fromEntries(names.map(name => {
  try { return [name, fs.readFileSync('/proc/sys/' + name, 'utf8').trim()]; }
  catch (error) { if (error.code === 'ENOENT') return [name, null]; throw error; }
}));
fs.writeFileSync('/output/sysctls-' + phase + '.json', JSON.stringify(values, null, 2) + '\n', { flag: 'wx' });
if (phase === 'after') {
  const before = JSON.parse(fs.readFileSync('/output/sysctls-before.json', 'utf8'));
  if (names.some(name => values[name] !== before[name])) throw new Error('observed global kernel sentinels changed');
}
