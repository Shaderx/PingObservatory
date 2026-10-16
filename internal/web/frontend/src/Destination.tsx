import type { DestinationState } from './types';
import { historyDate } from './history-windows';

export function DestinationDetails({ destination }: { destination?: DestinationState }) {
  return <details className="destination-history">
    <summary>Local destination {destination && destination.change_count > 0 && <span className="peer-flag">{destination.change_count} observed changes</span>}</summary>
    {destination ? <>
      <p>Last attempted address <code>{destination.last_ip}</code><br/>Captured at batch completion: {historyDate(destination.last_observed_at)}</p>
      <p>First captured baseline <code>{destination.baseline_ip}</code><br/>{historyDate(destination.baseline_observed_at)}</p>
      <p>Informational only. An attempted address may not reply. DNS and CDN rotation can be normal; destination changes do not alter endpoint health.</p>
      <ol aria-label="Recent local destination changes">{destination.history.map(change => <li key={change.observed_at}><time>{historyDate(change.observed_at)}</time><br/><code>{change.old_ip}</code> to <code>{change.new_ip}</code></li>)}</ol>
      <p>{destination.history.length} retained changes shown / {destination.change_count} changes since this baseline. Up to {destination.history_limit} recent changes are kept within local retention. Inactive series expire with their history.</p>
    </> : <p>No local destination has been captured. Catalogue evidence does not seed this history.</p>}
  </details>;
}
