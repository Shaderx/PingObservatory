/** Connector geometry and arrival motion for the local atlas.
 * No map, DOM, or layout reads in the geometry or animation loop.
 * Host owns eligibility, visibility, queue fairness, and cached measurements.
 */
export type XY = { x: number; y: number };
export type SignalRect = { left: number; top: number; width: number; height: number };
export type SignalPath = { d: string; length: number; points: readonly XY[] };
export type ArrivalGeometry = {
  connector: SignalPath;
  branches: readonly [SignalPath, SignalPath];
  entry: XY;
  convergence: XY;
};
export const SIGNAL_TIMING = Object.freeze({
  launch: 900, split: 1180, converge: 1500, flash: 1520, end: 1800,
});
const clamp = (n: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, n));
const mod = (n: number, m: number) => ((n % m) + m) % m;
const equals = (a: XY, b: XY) => Math.abs(a.x - b.x) < 0.001 && Math.abs(a.y - b.y) < 0.001;
function path(points: XY[]): SignalPath {
  const clean = points.filter((p, i) => !i || !equals(p, points[i - 1]));
  return {
    points: clean,
    d: clean.map((p, i) => `${i ? 'L' : 'M'}${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' '),
    length: clean.reduce((n, p, i) => n + (i ? Math.hypot(p.x - clean[i - 1].x, p.y - clean[i - 1].y) : 0), 0),
  };
}

/** Matches the actual leader(group) in ProbeAtlas, including its 8px elbow. */
export function connectorToBody(node: XY, body: SignalRect): SignalPath {
  const { left: l, top: t, width: w, height: h } = body;
  const r = l + w, b = t + h;
  let ax = clamp(node.x, l, r), ay = clamp(node.y, t, b);
  if (node.x > l && node.x < r && node.y > t && node.y < b) {
    const edges = [
      { distance: node.x - l, x: l, y: node.y },
      { distance: r - node.x, x: r, y: node.y },
      { distance: node.y - t, x: node.x, y: t },
      { distance: b - node.y, x: node.x, y: b },
    ].sort((a, z) => a.distance - z.distance);
    ax = edges[0].x; ay = edges[0].y;
  }
  const vertical = ax === l || ax === r;
  const elbow = {
    x: vertical ? ax + Math.sign(node.x - ax) * Math.min(8, Math.abs(node.x - ax)) : ax,
    y: vertical ? ay : ay + Math.sign(node.y - ay) * Math.min(8, Math.abs(node.y - ay)),
  };
  return path([node, elbow, { x: ax, y: ay }]);
}

/** Build the two DISJOINT exterior routes to opposite sides of the active row.
 * Both then turn inward along that row's bottom padding to its center.
 * No route crosses another provider's text, even if the entry is below the row.
 * Geometry uses measured row bounds; a row index / constant row height is wrong.
 */
export function buildArrivalGeometry(node: XY, body: SignalRect, row: SignalRect): ArrivalGeometry | undefined {
  if (![node.x, node.y, body.left, body.top, body.width, body.height, row.left, row.top, row.width, row.height].every(Number.isFinite)
    || body.width < 4 || body.height < 4 || row.width < 4 || row.height < 4
    || row.top < body.top - 1 || row.top + row.height > body.top + body.height + 1) return;
  const connector = connectorToBody(node, body), entry = connector.points.at(-1)!;
  const pad = 1.25;
  const l = body.left - pad, t = body.top - pad, r = body.left + body.width + pad, b = body.top + body.height + pad;
  const w = r - l, h = b - t, perimeter = 2 * (w + h);
  // Keep the original connector endpoint, then step outward by 1.25px.
  const exterior = { ...entry };
  if (entry.x === body.left) exterior.x = l;
  else if (entry.x === body.left + body.width) exterior.x = r;
  else if (entry.y === body.top) exterior.y = t;
  else exterior.y = b;
  const parameter = (p: XY) => {
    if (p.y === t) return p.x - l;
    if (p.x === r) return w + p.y - t;
    if (p.y === b) return w + h + r - p.x;
    return 2 * w + h + b - p.y;
  };
  const pointAt = (raw: number): XY => {
    const s = mod(raw, perimeter);
    if (s <= w) return { x: l + s, y: t };
    if (s <= w + h) return { x: r, y: t + s - w };
    if (s <= 2 * w + h) return { x: r - (s - w - h), y: b };
    return { x: l, y: b - (s - 2 * w - h) };
  };
  const underlineY = clamp(row.top + row.height - 0.75, body.top + 1, body.top + body.height - 0.5);
  const convergence = { x: clamp(row.left + row.width / 2, body.left + 1, body.left + body.width - 1), y: underlineY };
  const ports = [{ x: l, y: underlineY }, { x: r, y: underlineY }];
  const start = parameter(exterior);
  const cwDistance = (port: XY) => mod(parameter(port) - start, perimeter);
  // The first port encountered clockwise belongs to that branch. The other
  // belongs to the counterclockwise branch: the perimeter arcs never overlap.
  ports.sort((a, z) => cwDistance(a) - cwDistance(z));
  const corners = [0, w, w + h, 2 * w + h];
  const branch = (port: XY, direction: 1 | -1): SignalPath => {
    const end = parameter(port), distance = mod(direction * (end - start), perimeter);
    const intermediate = corners.map(s => ({ s, distance: mod(direction * (s - start), perimeter) }))
      .filter(v => v.distance > 0.001 && v.distance < distance - 0.001)
      .sort((a, z) => a.distance - z.distance).map(v => pointAt(v.s));
    return path([entry, exterior, ...intermediate, port, convergence]);
  };
  return { connector, branches: [branch(ports[0], 1), branch(ports[1], -1)], entry, convergence };
}

export type SignalHandle = { cancel(): void; finished: Promise<boolean> };
const SVG_NS = 'http://www.w3.org/2000/svg';
/** A small tail made from five adjacent opacity bands, with a crisp 2.4px tip.
 * It follows path arc length (not a screen-space gradient), including corners.
 * No filters, shadows, per-frame callbacks, getTotalLength, or DOM geometry reads.
 */
export function playArrivalSignal(svg: SVGSVGElement, geometry: ArrivalGeometry, row: HTMLButtonElement): SignalHandle {
  const group = document.createElementNS(SVG_NS, 'g');
  group.classList.add('probe-signal');
  svg.append(group);
  const animations: Animation[] = [];
  const start = document.timeline.currentTime;
  let settled = false, resolve!: (completed: boolean) => void;
  const finished = new Promise<boolean>(done => { resolve = done; });
  const animate = (element: Element, frames: Keyframe[], options: KeyframeAnimationOptions) => {
    const a = element.animate(frames, { fill: 'none', ...options });
    if (typeof start === 'number') a.startTime = start;
    animations.push(a);
    return a;
  };
  const runner = (route: SignalPath, delay: number, duration: number, tailOut: number) => {
    if (route.length < 0.1) return;
    const widths = [2.4, 3, 5, 8, 12, 16];
    const opacities = [1, 0.88, 0.60, 0.35, 0.18, 0.065];
    // Short connectors get a shorter tail, so the whole connector never lights up.
    const scale = Math.min(1, Math.max(0.2, route.length / 120));
    const tail = widths.reduce((a, z) => a + z, 0) * scale;
    let behind = 0;
    widths.forEach((raw, i) => {
      const width = raw * scale, p = document.createElementNS(SVG_NS, 'path');
      p.setAttribute('d', route.d);
      p.setAttribute('class', i ? 'probe-signal-tail' : 'probe-signal-tip');
      p.setAttribute('opacity', String(opacities[i]));
      p.setAttribute('stroke-dasharray', `${width} ${route.length + tail * 2 + 1}`);
      const initial = behind + width;
      // Initial dash lives before the path; the bright head enters first.
      p.setAttribute('stroke-dashoffset', String(initial));
      group.append(p);
      animate(p, [
        { offset: 0, strokeDashoffset: String(initial) },
        { offset: duration / (duration + tailOut), strokeDashoffset: String(initial - route.length) },
        { offset: 1, strokeDashoffset: String(initial - route.length - tail) },
      ], { delay, duration: duration + tailOut, easing: 'linear' });
      behind += width;
    });
  };
  runner(geometry.connector, SIGNAL_TIMING.launch, SIGNAL_TIMING.split - SIGNAL_TIMING.launch, 50);
  geometry.branches.forEach(route => runner(route, SIGNAL_TIMING.split, SIGNAL_TIMING.converge - SIGNAL_TIMING.split, 40));
  // Underlying selected/hover styling is restored by the empty final keyframe.
  // There is no backwards fill, so the row is untouched before convergence.
  const flash = animate(row, [
    { offset: 0, backgroundColor: 'var(--signal-flash-soft)' },
    { offset: 0.16, backgroundColor: 'var(--signal-flash)' },
    { offset: 1 },
  ], { delay: SIGNAL_TIMING.flash, duration: SIGNAL_TIMING.end - SIGNAL_TIMING.flash, easing: 'cubic-bezier(.2,.6,.3,1)' });
  const provider = row.querySelector('.probe-provider');
  if (provider) animate(provider, [
    { offset: 0, color: 'var(--text)' }, { offset: 0.18, color: 'var(--text)' }, { offset: 1 },
  ], { delay: SIGNAL_TIMING.flash, duration: SIGNAL_TIMING.end - SIGNAL_TIMING.flash, easing: 'ease-out' });
  const settle = (completed: boolean) => {
    if (settled) return;
    settled = true;
    for (const a of animations) { a.onfinish = null; a.oncancel = null; a.cancel(); }
    group.remove();
    resolve(completed);
  };
  flash.onfinish = () => settle(true);
  flash.oncancel = () => settle(false);
  return { finished, cancel: () => settle(false) };
}
