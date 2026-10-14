import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const lock = JSON.parse(readFileSync('package-lock.json', 'utf8'));
const parts = ['The Ping Observatory - bundled third-party software notices\n\nMap geometry terms are in maps/LICENSE.txt. Device-local fonts require no downloaded font assets.'];
for (const [path, entry] of Object.entries(lock.packages).sort()) {
  if (!path || entry.dev || entry.optional || !existsSync(path)) continue;
  const files = readdirSync(path).filter(name => /^(licen[sc]e|copying|notice)(\.|$)/i.test(name));
  const info = JSON.parse(readFileSync(join(path, 'package.json'), 'utf8'));
  parts.push(`\n${'='.repeat(72)}\n${info.name} ${entry.version}\nLicense: ${info.license ?? 'See license text'}\n`);
  for (const file of files) parts.push(`${file}\n${readFileSync(join(path, file), 'utf8')}`);
}
writeFileSync('public/THIRD-PARTY-NOTICES.txt', parts.join('\n'));
