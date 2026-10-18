import { describe, expect, it, vi } from 'vitest';
import type { Map } from 'maplibre-gl';
import { MapMovementGate, resizeAtlasViewport, viewportMovementEvent } from './map-movement';
import { ENDPOINT_BOUNDS } from './map-viewport';

describe('owned viewport movement', () => {
  it('tags both actual Atlas calls separately without changing fit options', () => {
    const events: object[] = [], gate = new MapMovementGate();
    const map = {
      resize: vi.fn((event: object) => { events.push(event); expect(gate.start(event, false)).toBe('viewport'); expect(gate.end(event, false)).toBe(true); }),
      fitBounds: vi.fn((_bounds: unknown, _options: unknown, event: object) => { events.push(event); expect(gate.start(event, true)).toBe('viewport'); expect(gate.end(event, false)).toBe(true); }),
    } as unknown as Pick<Map, 'resize' | 'fitBounds'>;
    resizeAtlasViewport(map, ENDPOINT_BOUNDS, true);
    expect(events).toHaveLength(2); expect(events[0]).not.toBe(events[1]);
    expect(map.fitBounds).toHaveBeenCalledWith(ENDPOINT_BOUNDS, { padding: 0, duration: 0 }, events[1]);
    resizeAtlasViewport(map, ENDPOINT_BOUNDS, false);
    expect(map.resize).toHaveBeenCalledTimes(2); expect(map.fitBounds).toHaveBeenCalledOnce();
  });

  it('holds resize/refit at both moving flags and retires completed identities', () => {
    const gate = new MapMovementGate(), resize = viewportMovementEvent(), fit = viewportMovementEvent();
    expect(gate.start(resize, false)).toBe('viewport');
    expect(gate.geometryPending).toBe(true); expect(gate.gestureActive).toBe(false);
    expect(gate.end(resize, false)).toBe(true);
    expect(gate.start(fit, true)).toBe('viewport');
    expect(gate.end(fit, false)).toBe(true);
    expect(gate.start(fit, true)).toBeUndefined();
    expect(gate.end(fit, false)).toBe(false); expect(gate.geometryPending).toBe(false);
  });

  it('does not infer viewport ownership from missing originalEvent or a forged tag', () => {
    for (const event of [{}, { atlasViewportToken: {} }, { atlasViewportToken: true }]) {
      const gate = new MapMovementGate();
      expect(gate.start(event, true)).toBe('gesture');
      expect(gate.gestureActive).toBe(true);
      expect(gate.end({}, true)).toBe(false); expect(gate.gestureActive).toBe(true);
      expect(gate.end({}, false)).toBe(true);
    }
  });

  it('does not let stale/overlapping viewport end callbacks release newer geometry', () => {
    const gate = new MapMovementGate(), old = viewportMovementEvent(), current = viewportMovementEvent();
    gate.start(old, false); gate.end(old, false); gate.start(current, true);
    expect(gate.end(old, false)).toBe(false); expect(gate.geometryPending).toBe(true);
    const another = viewportMovementEvent(); gate.start(another, true);
    expect(gate.end(current, false)).toBe(false); expect(gate.geometryPending).toBe(true);
    expect(gate.end(another, false)).toBe(true);
  });

  it('lets real movement override ownership without letting a viewport end finish it', () => {
    const gate = new MapMovementGate(), owned = viewportMovementEvent();
    gate.start(owned, true);
    expect(gate.progress({ originalEvent: {} }, true)).toBe('gesture');
    expect(gate.gestureActive).toBe(true);
    expect(gate.end(owned, false)).toBe(false); expect(gate.gestureActive).toBe(true);
    expect(gate.end({ originalEvent: {} }, false)).toBe(true);
    expect(gate.geometryPending).toBe(false);
  });


  it.each([false, true])('retires a stopped older gesture at a fresh owned start with moving=%s', moving => {
    const gate = new MapMovementGate();
    gate.start({ originalEvent: {} }, true);
    const current = viewportMovementEvent();
    // resize.stop(false) can omit the old gesture's unowned moveend.
    expect(gate.start(current, moving)).toBe('viewport');
    expect(gate.gestureActive).toBe(false); expect(gate.geometryPending).toBe(true);
    expect(gate.end(current, false)).toBe(true);
  });

  it('does not let a pre-created delayed operation retire a newer gesture owner', () => {
    const gate = new MapMovementGate(), old = viewportMovementEvent();
    gate.start({ originalEvent: {} }, true);
    expect(gate.start(old, true)).toBe('viewport');
    expect(gate.gestureActive).toBe(true);
    expect(gate.end(old, false)).toBe(false); expect(gate.gestureActive).toBe(true);
    expect(gate.end({ originalEvent: {} }, false)).toBe(true);
  });


  it.each(['start', 'progress'])('refreshes an already-gated gesture owner on newer genuine %s evidence', method => {
    const gate = new MapMovementGate();
    gate.start({ originalEvent: {} }, true);
    const older = viewportMovementEvent();
    expect(gate[method as 'start' | 'progress']({ originalEvent: {} }, true)).toBeUndefined();
    expect(gate.start(older, true)).toBe('viewport');
    expect(gate.gestureActive).toBe(true);
    expect(gate.end(older, false)).toBe(false);
    expect(gate.end({ originalEvent: {} }, false)).toBe(true);
  });

  it('retires an end received before its start so delayed callbacks cannot rehold geometry', () => {
    const gate = new MapMovementGate(), old = viewportMovementEvent();
    expect(gate.end(old, false)).toBe(false);
    expect(gate.start(old, false)).toBeUndefined(); expect(gate.geometryPending).toBe(false);
  });

  it('gives original gesture evidence priority even when an owned token is present', () => {
    const gate = new MapMovementGate(), owned = viewportMovementEvent();
    expect(gate.start({ ...owned, originalEvent: {} }, true)).toBe('gesture');
    expect(gate.gestureActive).toBe(true);
  });
});
