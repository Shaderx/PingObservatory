// @vitest-environment jsdom
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type * as maplibregl from 'maplibre-gl';
import { ArrivalLayoutGate, sameArrivalBounds, type ArrivalBounds } from './arrival-layout';
import { batchArrivalKey } from './batch-arrivals';
import { DemandPacer } from './demand-pacing';
import { ProbeAtlas } from './ProbeAtlas';
import { viewportMovementEvent } from './map-movement';
import type { Family, Observation, Target } from './types';
import { emptyObservation } from './model';

const control = vi.hoisted(() => ({
  markers: [] as HTMLElement[],
  signals: [] as Array<{ id: string; started: number; value: string; resolved: boolean; cancel: ReturnType<typeof vi.fn>; finish(completed: boolean): void }>,
}));
vi.mock('./arrival-signal', async importOriginal => {
  const actual = await importOriginal<typeof import('./arrival-signal')>();
  return { ...actual, playArrivalSignal: (_svg: SVGSVGElement, _shape: unknown, row: HTMLButtonElement) => {
    let finish!: (completed: boolean) => void;
    const finished = new Promise<boolean>(resolve => { finish = resolve; });
    // Keep cancellation completion under test control to exercise stale owners.
    const cancel = vi.fn();
    const signal = { id: row.dataset.targetId!, started: performance.now(), value: row.querySelector('.probe-rtt')?.textContent ?? '', resolved: false,
      cancel, finish: (completed: boolean) => { signal.resolved = true; finish(completed); } };
    control.signals.push(signal);
    return { finished, cancel };
  } };
});
vi.mock('maplibre-gl', () => ({ Marker: class {
  private coordinates: unknown;
  constructor(private options: { element: HTMLElement }) { control.markers.push(options.element); }
  setLngLat(coordinates: unknown) { this.coordinates = coordinates; return this; }
  getLngLat() { return this.coordinates; }
  addTo() { return this; }
  remove() {}
  getElement() { return this.options.element; }
} }));

describe('committed arrival layout gate', () => {
  it('requires geometry and same-snapshot admission and rejects stale epochs', () => {
    const gate = new ArrivalLayoutGate<object>(), first = {}, second = {};
    gate.holdSnapshot(first);
    const oldEpoch = gate.epoch;
    gate.commitGeometry(oldEpoch, true);
    expect(gate.canDrain(first)).toBe(false);
    gate.admit(oldEpoch, first);
    expect(gate.canDrain(first)).toBe(true);
    gate.holdSnapshot(second);
    gate.commitGeometry(oldEpoch, true);
    gate.admit(oldEpoch, first);
    expect(gate.canDrain(second)).toBe(false);
    gate.commitGeometry(gate.epoch, true);
    gate.admit(gate.epoch, second);
    expect(gate.canDrain(second)).toBe(true);
    gate.invalidateGeometry();
    expect(gate.canDrain(second)).toBe(false);
  });

  it('compares every raw extent and row identity against a fixed active baseline', () => {
    const first: ArrivalBounds = { node: { x: 5, y: 6 }, body: { left: 20, top: 10, width: 150, height: 24 },
      row: { left: 20, top: 10, width: 150, height: 24 }, element: document.createElement('button') };
    expect(sameArrivalBounds(first, { ...first, body: { ...first.body, width: 150.4 } })).toBe(true);
    expect(sameArrivalBounds(first, { ...first, body: { ...first.body, width: 150.8 } })).toBe(false);
    expect(sameArrivalBounds(first, { ...first, row: { ...first.row, height: 25 } })).toBe(false);
    expect(sameArrivalBounds(first, { ...first, element: document.createElement('button') })).toBe(false);
    expect(sameArrivalBounds(first, undefined)).toBe(false);
  });
});

const base = Date.parse('2026-10-11T08:00:00Z');
type Body = { left: number; top: number; width: number; height: number };
let root: Root | undefined, host: HTMLDivElement, targets: Target[], registered: Set<string>;
let arrivals: ReadonlyMap<string, number>, family: Family, motion: boolean, moving: boolean, reduced: boolean;
let viewport: { clientWidth: number; clientHeight: number }, svgUsable: boolean;
let listeners: Map<string, Set<(event?: object) => void>>, observers: Array<{ notify(ids: string[]): void }>, bodies: Map<string, Body>;
const observation = (at: number, overrides: Partial<Observation> = {}): Observation => ({
  ...emptyObservation('icmp','ipv4'), status:'ok', observed_at:new Date(at).toISOString(),
  sent:3,received:3,attempted:3,median_ms:15,min_ms:14,max_ms:16,baseline_ms:14,
  baseline_sample_count:5,baseline_as_of:new Date(at).toISOString(),rolling_as_of:new Date(at).toISOString(),loss_pct:0,...overrides,
});
const target = (id: string): Target => ({
 id,revision:1,host:'fixture.example',label:id,provider:'Test',category:'regional',role:'test',region:id,country_code:'US',world_area:'Test',
 geography:{coordinates:[0,0]}, enabled:true,measurement:{method:'icmp',families:['ipv4'],interval_seconds:60,packets_per_cycle:3,packet_spacing_ms:100,timeout_ms:1000,payload_bytes:32},observations:[observation(base-2000)],
});
const groupId = (element: Element) => (element.closest('.probe-callout') as HTMLElement | null)?.dataset.groupId ?? '';
const bodyFor = (element: Element) => bodies.get(groupId(element)) ?? { left: 10, top: 20, width: 160, height: 24 };
const activeSignals = () => control.signals.filter(signal => !signal.resolved && signal.cancel.mock.calls.length === 0);
const map = () => ({
  getContainer: () => ({ get clientWidth() { return viewport.clientWidth; }, get clientHeight() { return viewport.clientHeight; }, closest: () => null, querySelectorAll: () => [], getBoundingClientRect: () => new DOMRect(0,0,viewport.clientWidth,viewport.clientHeight) }), project: () => ({ x: 300, y: 160 }), isMoving: () => moving,
  on: (name: string, fn: (event?: object) => void) => { const set = listeners.get(name) ?? new Set(); set.add(fn); listeners.set(name, set); },
  off: (name: string, fn: (event?: object) => void) => listeners.get(name)?.delete(fn),
}) as unknown as maplibregl.Map;
let currentMap: maplibregl.Map;
let mediaUpdates: Set<() => void>;
const render = async (flush = true) => {
  await act(async () => { root!.render(createElement(ProbeAtlas, {
    map: currentMap, targets, registeredTargetIds: registered, selected: '', onSelect() {},
    family, now: Date.now(), arrivals, motionEnabled: motion, theme: 'dark',
  })); });
  if (flush) await tick(16);
};
const tick = async (ms: number) => { await act(async () => { await vi.advanceTimersByTimeAsync(ms); }); };
const update = (ids: string[], overrides: Partial<Observation> = {}) => {
  const clock = Date.now();
  targets = targets.map(item => ids.includes(item.id) ? { ...item, observations: [observation(clock, overrides)] } : item);
  arrivals = new Map(ids.map(id => { const item = targets.find(t => t.id === id)!; return [batchArrivalKey(id, 'ipv4', item.measurement.method, item.revision), clock]; }));
  return clock;
};
const emit = async (name: string, event: object = {}) => { await act(async () => { for (const fn of listeners.get(name) ?? []) fn(event); }); };
const resizeLabels = async (ids: string[]) => { await act(async () => { for (const observer of observers) observer.notify(ids); }); };

describe('arrival relayout integration', () => {
  beforeEach(async () => {
    vi.useFakeTimers({ toFake: ['Date', 'performance', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] }); vi.setSystemTime(base);
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(() => callback(performance.now()), 16));
    vi.stubGlobal('cancelAnimationFrame', (id: number) => window.clearTimeout(id));
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    family = 'ipv4'; motion = false; moving = false; reduced = false; svgUsable = true;
    mediaUpdates = new Set();
    viewport = { clientWidth: 640, clientHeight: 320 }; listeners = new Map(); observers = []; bodies = new Map();
    targets = ['a', 'b', 'c', 'd'].map(target); registered = new Set(targets.map(item => item.id)); arrivals = new Map();
    targets.forEach((item, index) => bodies.set('US:' + item.id, { left: 10 + index * 180, top: 20, width: 160, height: 24 }));
    vi.stubGlobal('matchMedia', () => ({ get matches() { return reduced; }, addEventListener: (_name: string, fn: () => void) => mediaUpdates.add(fn), removeEventListener: (_name: string, fn: () => void) => mediaUpdates.delete(fn) }));
    vi.stubGlobal('ResizeObserver', class {
      private elements = new Set<Element>();
      constructor(private callback: ResizeObserverCallback) {
        observers.push({ notify: ids => this.callback([...this.elements].filter(element => ids.includes((element as HTMLElement).dataset.groupId ?? '')).map(target => ({ target }) as ResizeObserverEntry), this as unknown as ResizeObserver) });
      }
      observe(element: Element) { this.elements.add(element); }
      unobserve(element: Element) { this.elements.delete(element); }
      disconnect() { this.elements.clear(); }
    });
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      if ((this.classList.contains('probe-signals') || this.classList.contains('probe-atlas'))) return new DOMRect(0, 0, svgUsable ? viewport.clientWidth : 0, viewport.clientHeight);
      const body = bodyFor(this);
      return new DOMRect(body.left, body.top, body.width, body.height);
    });
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) { return bodyFor(this).width; });
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) { return bodyFor(this).height + (this.classList.contains('probe-callout') ? 15 : 0); });
    vi.spyOn(HTMLElement.prototype, 'offsetTop', 'get').mockReturnValue(15);
    vi.spyOn(HTMLElement.prototype, 'offsetLeft', 'get').mockReturnValue(0);
    Object.defineProperty(Element.prototype, 'getAnimations', { configurable: true, value: () => [] });
    host = document.createElement('div'); document.body.append(host); root = createRoot(host); currentMap = map();
    // Saved readings never create startup arrivals.
    await render(); motion = true;
  });
  afterEach(async () => {
    if (root) await act(async () => { root!.unmount(); });
    root = undefined;
    vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals();
    delete (Element.prototype as unknown as { getAnimations?: unknown }).getAnimations;
    control.signals.length = 0; control.markers.length = 0; document.body.innerHTML = '';
  });

  const reachStarts = async (count: number) => {
    const deadline = Date.now() + 14000;
    while (control.signals.length < count && Date.now() < deadline) await tick(16);
    expect(control.signals).toHaveLength(count);
  };

  it('stagger-launches one sequence per frame and preserves unique source ownership', async () => {
    const accepted = update(['a', 'b', 'c', 'd']); await render();
    expect(control.signals.map(signal => signal.id)).toEqual(['a']);
    await render(); // Repeating the same input cannot replay it.
    await reachStarts(4);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b', 'c', 'd']);
    const starts = control.signals.map(signal => signal.started);
    expect(new Set(starts).size).toBe(4);
    expect(starts.every((start, index) => index === 0 || start - starts[index - 1] >= 16)).toBe(true);
    expect(activeSignals()).toHaveLength(4);
    expect(Date.now() - accepted).toBeLessThan(15000);
  });

  it('retains FIFO and unaffected owners while canceling an unreported repacked owner', async () => {
    const accepted = update(['a', 'b', 'c', 'd']); await render(); await reachStarts(2);
    bodies.get('US:a')!.left += 10;
    bodies.get('US:d')!.width += 10;
    await resizeLabels(['US:d']); await tick(16);
    expect(control.signals[0].cancel).toHaveBeenCalledOnce();
    expect(control.signals[1].cancel).not.toHaveBeenCalled();
    await reachStarts(4);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b', 'c', 'd']);
    expect(control.signals.slice(2).every(signal => signal.cancel.mock.calls.length === 0)).toBe(true);
    expect(Date.now() - accepted).toBeLessThan(15000);
  });

  it('coalesces newer sources before deferred geometry validation without resetting original age', async () => {
    const accepted = update(['a', 'b', 'c', 'd']); await render(); await tick(100);
    const source = update(['b'], { median_ms: 100.1 }); bodies.get('US:b')!.width += 8;
    await render(false); await resizeLabels(['US:b']); await tick(16);
    await reachStarts(2);
    expect(control.signals[1]).toMatchObject({ id: 'b', value: '100.1' });
    expect(control.signals.filter(signal => signal.id === 'b')).toHaveLength(1);
    expect(Date.now() - accepted).toBeLessThan(15000);
    expect(source).toBeGreaterThan(accepted);
  });

  it('expires coalesced work at its original deadline instead of extending its lifetime', async () => {
    const accepted = update(['a', 'b']); await render(); await tick(100);
    const cycle = viewportMovementEvent(); moving = true; await emit('movestart', cycle);
    await tick(14000 - (Date.now() - accepted));
    update(['b'], { median_ms: 100.1 }); await render(false);
    await tick(1001);
    moving = false; await emit('moveend', cycle); await tick(2000);
    expect(control.signals.map(signal => signal.id)).toEqual(['a']);
    update(['b'], { median_ms: 100.2 }); await render();
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b']);
    expect(control.signals[1].value).toBe('100.2');
  });

  it('rejects canceled frame callbacks and stale completions without releasing a newer owner', async () => {
    update(['a']); await render(); const old = control.signals[0];
    bodies.get('US:a')!.width += 10; await resizeLabels(['US:a']);
    expect(old.cancel).toHaveBeenCalledOnce();
    await tick(100); update(['a']); await render(false);
    await emit('resize'); await emit('resize'); await tick(16);
    expect(control.signals).toHaveLength(2);
    await act(async () => { old.finish(false); }); await tick(16);
    expect(control.signals[1].cancel).not.toHaveBeenCalled();
    expect(control.signals).toHaveLength(2);
    expect(activeSignals()).toHaveLength(1);
  });

  it.each(['resize', 'fullscreen'])('preserves original FIFO across owned %s/refit with demand pacing', async mode => {
    const accepted = update(['a', 'b', 'c', 'd']); await render(); await tick(100);
    const resize = viewportMovementEvent(), refit = viewportMovementEvent();
    viewport.clientWidth = mode === 'fullscreen' ? 1440 : 1280;
    await emit('movestart', resize); await emit('move', resize);
    await emit('resize', resize); await emit('moveend', resize);
    moving = true; await emit('movestart', refit); await emit('move', refit);
    moving = false; await emit('moveend', refit); await tick(16);
    await reachStarts(4);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b', 'c', 'd']);
    expect(control.signals.slice(1).every(signal => signal.cancel.mock.calls.length === 0)).toBe(true);
    expect(Date.now() - accepted).toBeLessThan(15000);
  });

  it('holds combined source/layout changes and completions behind the same-snapshot geometry gate', async () => {
    const accepted = update(['a', 'b', 'c']); await render(); await tick(100);
    const cycle = viewportMovementEvent(); moving = true; await emit('movestart', cycle);
    const source = update(['b'], { median_ms: 100.1 }); bodies.get('US:b')!.width += 8;
    await render(false); await resizeLabels(['US:b']); await tick(16);
    await act(async () => { control.signals[0].finish(true); }); await tick(2000);
    expect(control.signals).toHaveLength(1);
    moving = false; await emit('moveend', cycle); await tick(16);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b']);
    expect(control.signals[1]).toMatchObject({ id: 'b', value: '100.1' });
    expect(control.signals[1].cancel).not.toHaveBeenCalled();
    expect(Date.now() - accepted).toBeLessThan(15000);
    expect(source).toBeGreaterThan(accepted);
  });

  it('retains original pending work through a wall-clock jump before its monotonic deadline', async () => {
    const accepted = update(['a', 'b', 'c']); await render(); await tick(100);
    const cycle = viewportMovementEvent(); moving = true; await emit('movestart', cycle);
    const wallBeforeJump = Date.now();
    vi.setSystemTime(wallBeforeJump + 3600000);
    await tick(1000);
    expect(control.signals.map(signal => signal.id)).toEqual(['a']);
    vi.setSystemTime(wallBeforeJump + 1000); // Restore source freshness, not elapsed time.
    moving = false; await emit('moveend', cycle); await tick(16);
    await reachStarts(3);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b', 'c']);
    expect(Date.now() - accepted).toBeLessThan(15000);
  });

  it('expires original monotonic ages while geometry is blocked, including after a wall-clock jump', async () => {
    const accepted = update(['a', 'b', 'c', 'd']); await render(); await tick(100);
    const cycle = viewportMovementEvent(); moving = true; await emit('movestart', cycle);
    const elapsed = Date.now() - accepted;
    vi.setSystemTime(Date.now() + 3600000); // This must not advance an elapsed deadline.
    await tick(15000 - elapsed - 1);
    expect(control.signals).toHaveLength(1);
    await tick(1);
    vi.setSystemTime(accepted + 15000); // Keep observations fresh when the geometry gate resumes.
    moving = false; await emit('moveend', cycle); await tick(2000);
    expect(control.signals.map(signal => signal.id)).toEqual(['a']);
    update(['b']); await render();
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b']);
  });

  it('expires a head that crosses its original deadline during frame preparation', async () => {
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.push(callback); return frames.length; });
    vi.stubGlobal('cancelAnimationFrame', () => {});
    update(['a']); await render(false);
    await tick(14999); // The expiry timer cannot interrupt the following synchronous callback.
    const nativeNow = performance.now.bind(performance);
    let preparationMs = 0;
    vi.spyOn(performance, 'now').mockImplementation(() => nativeNow() + preparationMs);
    const originalPlan = DemandPacer.prototype.plan;
    vi.spyOn(DemandPacer.prototype, 'plan').mockImplementation(function (this: DemandPacer, ...args) {
      const plan = originalPlan.apply(this, args);
      preparationMs = 2;
      return plan;
    });
    await act(async () => { frames.at(-1)!(performance.now()); });
    expect(control.signals).toHaveLength(0);
    await tick(16);
    expect(control.signals).toHaveLength(0);
  });

  it('rejects retired viewport callbacks and retains genuine interruption during a refit', async () => {
    update(['a', 'b', 'c']); await render(); await tick(100);
    const old = viewportMovementEvent(), current = viewportMovementEvent();
    await emit('movestart', old); await emit('moveend', old);
    moving = true; await emit('movestart', current);
    await emit('moveend', old); await tick(1500);
    expect(control.signals).toHaveLength(1);
    await emit('move', { originalEvent: new WheelEvent('wheel') });
    expect(control.signals[0].cancel).toHaveBeenCalledOnce();
    moving = false; await emit('moveend', current);
    update(['b']); await render(); expect(control.signals).toHaveLength(1);
    await emit('moveend', { originalEvent: new MouseEvent('mouseup') }); await tick(16);
    update(['b']); await render();
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b']);
    await act(async () => { control.signals[0].finish(false); }); await tick(16);
    expect(control.signals[1].cancel).not.toHaveBeenCalled();
  });

  it.each(['resize', 'refit'])('recovers when owned %s suppresses a genuine handler end', async mode => {
    update(['a', 'b', 'c']); await render();
    moving = true; await emit('movestart', { originalEvent: new MouseEvent('mousemove') });
    expect(control.signals[0].cancel).toHaveBeenCalledOnce();
    const cycle = viewportMovementEvent(); moving = mode === 'refit';
    await emit('movestart', cycle); await emit('resize', cycle); await emit('move', cycle);
    moving = false; await emit('moveend', cycle); await tick(2000);
    expect(control.signals.map(signal => signal.id)).toEqual(['a']);
    update(['a', 'b']); await render();
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'a']);
    await act(async () => { control.signals[0].finish(false); }); await tick(16);
    expect(control.signals[1].cancel).not.toHaveBeenCalled();
  });

  it.each(['pan', 'zoom', 'keyboard'])('cancels genuine %s work instead of replaying it', async kind => {
    update(['a', 'b', 'c']); await render();
    const originalEvent = kind === 'pan' ? new MouseEvent('mousemove')
      : kind === 'zoom' ? new WheelEvent('wheel') : new KeyboardEvent('keydown', { key: 'ArrowRight' });
    moving = true; await emit('movestart', { originalEvent });
    expect(control.signals[0].cancel).toHaveBeenCalledOnce();
    moving = false; await emit('moveend', { originalEvent }); await tick(16);
    expect(control.signals).toHaveLength(1);
  });

  it('keeps a busy location head while allowing other locations to launch', async () => {
    targets = targets.map(item => item.id === 'a' || item.id === 'd' ? { ...item, region: 'shared' } : item);
    bodies.set('US:shared', { left: 10, top: 20, width: 160, height: 80 });
    update(['a']); await render();
    update(['b', 'c', 'd']); await render();
    await reachStarts(3);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b', 'c']);
    await act(async () => { control.signals[0].finish(true); }); await tick(16);
    await reachStarts(4);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b', 'c', 'd']);
    expect(control.signals.filter(signal => signal.id === 'd')).toHaveLength(1);
  });

  it('grows pending retention with the registry and explicitly prunes withdrawn identities', async () => {
    viewport.clientHeight = 2000;
    targets = Array.from({ length: 45 }, (_, i) => target('t' + String(i).padStart(2, '0')));
    registered = new Set(targets.map(item => item.id));
    // Every mocked row is inside the list viewport. Off-screen rows intentionally
    // consume arrivals silently rather than spending an invisible animation lane.
    targets.forEach((item, index) => bodies.set('US:' + item.id, { left: 10 + index % 3 * 180, top: 20 + Math.floor(index / 3) * 40, width: 160, height: 24 }));
    const accepted = update(targets.map(item => item.id)); await render();
    expect(control.signals.map(signal => signal.id)).toEqual(['t00']);
    await tick(100); const source = update(['t40']); await render();
    expect(source).toBeGreaterThan(accepted);
    registered = new Set([...registered].filter(id => id !== 't40')); await render();
    expect(control.signals.some(signal => signal.id === 't40')).toBe(false);
    await tick(1800);
    expect(activeSignals().length).toBeGreaterThan(5);
    await reachStarts(44);
    expect(new Set(control.signals.map(signal => signal.id)).size).toBe(44);
    expect(control.signals.some(signal => signal.id === 't40')).toBe(false);
    expect(Date.now() - accepted).toBeLessThan(15000);
  });

  it('rotates actual service across repeated cohorts larger than one location can finish before expiry', async () => {
    viewport.clientHeight = 2000;
    targets = Array.from({ length: 16 }, (_, i) => ({ ...target('t' + String(i).padStart(2, '0')), region: 'shared' }));
    registered = new Set(targets.map(item => item.id));
    bodies.set('US:shared', { left: 10, top: 20, width: 160, height: 400 });
    for (let cohort = 0; cohort < 2; cohort++) {
      update(targets.map(item => item.id)); await render();
      const until = Date.now() + 16500;
      while (Date.now() < until) {
        await tick(1800);
        for (const signal of activeSignals()) await act(async () => signal.finish(true));
        await tick(16);
      }
    }
    expect(new Set(control.signals.map(signal => signal.id)).size).toBe(16);
    expect(control.signals.length).toBeLessThan(32); // expired visuals are dropped, never replayed.
  });

  it('keeps monotonic FIFO when wall time moves backward before a new admission', async () => {
    targets = targets.map(item => ({ ...item, region: 'shared' }));
    bodies.set('US:shared', { left: 10, top: 20, width: 160, height: 100 });
    update(['a', 'b'], { observed_at: new Date(base - 5000).toISOString() }); await render();
    await tick(100); vi.setSystemTime(base - 1000);
    update(['c'], { observed_at: new Date(base - 4000).toISOString() }); await render();
    await act(async () => control.signals[0].finish(true)); await tick(1500);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b']);
    await act(async () => control.signals[1].finish(true)); await tick(1500);
    expect(control.signals.map(signal => signal.id)).toEqual(['a', 'b', 'c']);
  });

  it('retains family/motion/hidden/reduced-motion cancellation without catch-up replay', async () => {
    update(['a', 'b', 'c']); await render();
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); }); await tick(1000);
    expect(control.signals[0].cancel).toHaveBeenCalledOnce();
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
    update(['a']); await render(); expect(control.signals).toHaveLength(2);
    family = 'ipv6'; await render();
    expect(control.signals[1].cancel).toHaveBeenCalledOnce();
    family = 'ipv4'; await render();
    update(['a']); await render();
    motion = false; await render();
    expect(control.signals.at(-1)!.cancel).toHaveBeenCalledOnce();
    motion = true; update(['a']); await render();
    reduced = true; await act(async () => { for (const fn of mediaUpdates) fn(); });
    expect(control.signals.at(-1)!.cancel).toHaveBeenCalledOnce();
    reduced = false; await act(async () => { for (const fn of mediaUpdates) fn(); });
    update(['a']); await render(); await act(async () => { root!.unmount(); }); root = undefined;
    expect(control.signals.at(-1)!.cancel).toHaveBeenCalledOnce();
  });

  it('cancels old revisions and consumes filtered arrivals without replay on restore', async () => {
    update(['a', 'b']); await render();
    const old = control.signals[0];
    targets = targets.map(item => ({ ...item, revision: 2 })); await render();
    expect(old.cancel).toHaveBeenCalledOnce();
    await tick(2000); expect(control.signals).toHaveLength(1);
    update(['a']); await render(); expect(control.signals).toHaveLength(2);
    const restored = targets;
    targets = targets.filter(item => item.id !== 'a'); await render();
    targets = restored; await render(); await tick(2000);
    expect(control.signals).toHaveLength(2);
  });

  it.each([640, 1440])('keeps compact digit/error/null/loss presentation at viewport %i', async width => {
    viewport.clientWidth = width; await emit('resize'); await tick(16);
    const row = () => host.querySelector<HTMLButtonElement>('button[data-target-id="a"]')!;
    update(['a'], { median_ms: 999.9, loss_pct: 99.9, status: 'timeout', received: 0 }); await render();
    expect(row().querySelector('.probe-measurement-reserve')).toBeNull();
    expect(row().querySelector('.probe-rtt')!.textContent).toBe('999.9');
    expect(row().querySelector('.probe-condition')).toBeNull();
    expect(row().querySelector('.probe-unit')).toBeNull();
    expect(row().querySelector('.probe-status-pattern.failure')!.getAttribute('aria-hidden')).toBe('true');
    expect(control.markers.length).toBeGreaterThan(0);
    expect(control.markers.every(marker => !marker.querySelector('.probe-loss-ring'))).toBe(true);
    expect(control.markers.filter(marker => marker.classList.contains('probe-geodot')).every(marker => !!marker.querySelector('.probe-aura') && !!marker.querySelector('.probe-core'))).toBe(true);
    expect(control.markers.some(marker => marker.classList.contains('probe-arrival-layer') && !!marker.querySelector('.probe-ripple'))).toBe(true);
    expect(host.querySelector('.probe-signals')).not.toBeNull();
    expect(control.signals[0].id).toBe('a');
    const loss = row().querySelector<HTMLElement>('.probe-loss-bar')!;
    expect(Number(loss.style.getPropertyValue('--probe-loss-scale'))).toBeCloseTo(0.999);
    expect(loss.getAttribute('aria-hidden')).toBe('true');
    expect(row().getAttribute('aria-label')).toContain('99.9%');
    expect(row().getAttribute('aria-label')).toContain('milliseconds');
    expect(row().querySelector('.probe-method')!.textContent).toBe('I');
    expect(row().getAttribute('aria-label')).toContain('ICMP RTT');
    update(['a'], { median_ms: null, loss_pct: 0 }); await render();
    expect(row().querySelector('.probe-measurement-reserve')).toBeNull();
    expect(row().querySelector('.probe-rtt')!.textContent).toBe('\u2014');
    expect(row().querySelector('.probe-loss-bar')).toBeNull();
    update(['a'], { median_ms: 9.9, loss_pct: null }); await render();
    expect(row().querySelector('.probe-measurement-reserve')).toBeNull();
    expect(row().querySelector('.probe-loss-bar')).toBeNull();
    expect(row().querySelector('.probe-status-pattern.information')).not.toBeNull();
    expect(row().getAttribute('aria-label')).toContain('unavailable');
    targets = targets.map(item => item.id === 'a' ? { ...item, measurement: { ...item.measurement, method: 'udp_echo' } } : item);
    update(['a'], { method: 'udp_echo' }); await render();
    expect(row().querySelector('.probe-method')!.textContent).toBe('U');
    expect(row().getAttribute('aria-label')).toContain('UDP echo RTT');
  });
});
