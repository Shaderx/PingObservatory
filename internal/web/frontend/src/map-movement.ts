import type { LngLatBoundsLike, Map } from 'maplibre-gl';

type MovementEvent = { atlasViewportToken?: unknown; originalEvent?: unknown };
const viewportTokens = new WeakMap<object, number>();
let movementOrder = 0;
export function viewportMovementEvent() {
  const token = {};
  viewportTokens.set(token, ++movementOrder);
  return Object.freeze({ atlasViewportToken: token });
}
const viewportToken = (event?: MovementEvent) => {
  const token = event?.atlasViewportToken;
  return token !== null && typeof token === 'object' && viewportTokens.has(token) ? token : undefined;
};

/** Each owned resize/refit has its own identity; unrelated movement retains the
 * existing cancellation policy. Never infer ownership from a missing DOM event. */
export class MapMovementGate {
  private viewport = new Set<object>();
  private retired = new WeakSet<object>();
  private gesture = false;
  private gestureOrder = 0;
  get geometryPending() { return this.gesture || this.viewport.size > 0; }
  get gestureActive() { return this.gesture; }

  start(event: MovementEvent | undefined, moving: boolean): 'viewport' | 'gesture' | undefined {
    const token = viewportToken(event);
    if (token && !event?.originalEvent) {
      if (this.retired.has(token) || this.viewport.has(token)) return;
      this.viewport.add(token);
      // Both Atlas APIs stop prior handlers before emitting their owned start.
      // Creation order protects a newer gesture from a delayed older callback.
      if (this.gesture && viewportTokens.get(token)! > this.gestureOrder) this.gesture = false;
      return 'viewport';
    }
    if (!moving) return;
    const alreadyGated = this.gesture;
    this.gesture = true;
    this.gestureOrder = ++movementOrder;
    return alreadyGated ? undefined : 'gesture';
  }
  progress(event: MovementEvent | undefined, moving: boolean): 'gesture' | undefined {
    // A user handler can begin while a camera cycle is already in progress.
    if (event?.originalEvent && moving) {
      const alreadyGated = this.gesture;
      this.gesture = true;
      this.gestureOrder = ++movementOrder;
      return alreadyGated ? undefined : 'gesture';
    }
  }
  end(event: MovementEvent | undefined, moving: boolean) {
    const token = viewportToken(event);
    if (token && !event?.originalEvent) {
      const active = this.viewport.delete(token);
      this.retired.add(token);
      if (!active) return false;
      // A viewport callback cannot end a concurrent genuine gesture.
    } else {
      if (moving) return false;
      this.gesture = false;
    }
    return !moving && !this.geometryPending;
  }
}

export function resizeAtlasViewport(map: Pick<Map, 'resize' | 'fitBounds'>, bounds: LngLatBoundsLike, refit: boolean) {
  map.resize(viewportMovementEvent());
  if (refit) map.fitBounds(bounds, { padding: 0, duration: 0 }, viewportMovementEvent());
}
