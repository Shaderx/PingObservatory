import type { Config, LocalTarget } from './types';

export function prepareConfig(draft: Config, saved: Config): Config {
  if (draft.manual_targets.length > 16) throw new Error('Add at most 16 manual targets.');
  return { ...draft, manual_targets: draft.manual_targets.map(target => {
    const previous = saved.manual_targets.find(item => item.id === target.id);
    const changed = previous && (previous.host !== target.host || previous.label !== target.label || JSON.stringify(previous.location ?? null) !== JSON.stringify(target.location ?? null));
    const clean: LocalTarget = { id: target.id, revision: previous ? previous.revision + (changed ? 1 : 0) : 1, label: target.label.trim(), host: target.host.trim() };
    if (target.location) clean.location = target.location;
    return clean;
  }) };
}

export function newManualTarget(): LocalTarget {
  return { id: `manual-${crypto.randomUUID()}`, revision: 1, label: '', host: '' };
}
