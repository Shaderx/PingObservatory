import type { SignalRect, XY } from './arrival-signal';

/** Starts and snapshot-sensitive revalidation require a committed layout and
 * that layout's admission pass. Geometry invalidation never changes event age. */
export class ArrivalLayoutGate<Snapshot> {
  epoch = 0;
  private snapshot?: Snapshot;
  private usableGeometry = false;
  private admission = false;

  holdSnapshot(snapshot: Snapshot) {
    if (this.snapshot === snapshot) return;
    this.snapshot = snapshot;
    this.invalidateGeometry();
  }
  invalidateGeometry() {
    this.epoch++;
    this.usableGeometry = false;
    this.admission = false;
  }
  isCurrent(snapshot: Snapshot) { return this.snapshot === snapshot; }
  commitGeometry(epoch: number, usable: boolean) {
    if (epoch === this.epoch) this.usableGeometry = usable;
  }
  admit(epoch: number, snapshot: Snapshot) {
    if (epoch === this.epoch && this.isCurrent(snapshot)) this.admission = true;
  }
  geometryReady() { return this.usableGeometry; }
  canDrain(snapshot: Snapshot) { return this.isCurrent(snapshot) && this.usableGeometry && this.admission; }
}

export type ArrivalBounds = { node: XY; body: SignalRect; row: SignalRect; element: HTMLButtonElement };
const near = (a: number, b: number) => Math.abs(a - b) <= 0.5;
const sameRect = (a: SignalRect, b: SignalRect) =>
  near(a.left, b.left) && near(a.top, b.top) && near(a.width, b.width) && near(a.height, b.height);

/** Compare the full inputs, not only paths: changed row/body extents can leave
 * the connector endpoints unchanged. The active baseline stays fixed. */
export function sameArrivalBounds(a: ArrivalBounds | undefined, b: ArrivalBounds | undefined) {
  return !!a && !!b && a.element === b.element && near(a.node.x, b.node.x) && near(a.node.y, b.node.y)
    && sameRect(a.body, b.body) && sameRect(a.row, b.row);
}
