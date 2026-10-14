export type TargetGroupRows = { ids: string[] };

export function retainCurrentTargetGroups<Group extends TargetGroupRows>(groups: readonly Group[], currentTargetIds: ReadonlySet<string>): Group[] {
  return groups.flatMap(group => {
    const ids = group.ids.filter(id => currentTargetIds.has(id));
    return ids.length ? [{ ...group, ids }] : [];
  });
}
