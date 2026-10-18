import { useEffect, useRef, useState, type CSSProperties } from 'react';
import * as maplibregl from 'maplibre-gl';
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url';
import 'maplibre-gl/dist/maplibre-gl.css';
import type { Family, Target } from './types';
import { ENDPOINT_BOUNDS, MAP_ASPECT_RATIO } from './map-viewport';
import { ProbeAtlas } from './ProbeAtlas';
import { resizeAtlasViewport } from './map-movement';

// Vite must bundle MapLibre 6's module worker and shared imports on this origin.
maplibregl.setWorkerUrl(workerUrl);

export function Atlas({ targets, registeredTargetIds, selected, onSelect, family, now, arrivals, motionEnabled, onMotionEnabledChange, theme, origin, accessAllowanceMS }: { targets: Target[]; registeredTargetIds: ReadonlySet<string>; selected: string; onSelect: (id: string) => void; family: Family; now: number; arrivals: ReadonlyMap<string, number>; motionEnabled: boolean; onMotionEnabledChange(enabled: boolean): void; theme: string; origin?: [number, number]; accessAllowanceMS?: number }) {
  const container = useRef<HTMLDivElement>(null), map = useRef<maplibregl.Map | null>(null);
  const wrapper = useRef<HTMLDivElement>(null), maximizeButton = useRef<HTMLButtonElement>(null);
  const mounted = useRef(true), fullscreenRequest = useRef(false);
  const [nativeFullscreen, setNativeFullscreen] = useState(false), [expandedFallback, setExpandedFallback] = useState(false);
  const expanded = nativeFullscreen || expandedFallback;
  const wasExpanded = useRef(false);
  const [frameWidth, setFrameWidth] = useState(0);
  useEffect(() => {
    if (!wrapper.current) return;
    const observer = new ResizeObserver(entries => setFrameWidth(entries[0].contentRect.width));
    observer.observe(wrapper.current); return () => observer.disconnect();
  }, []);
  const [ready, setReady] = useState(false), [failed, setFailed] = useState(false);
  useEffect(() => {
    mounted.current = true;
    const changed = () => {
      const ownsFullscreen = document.fullscreenElement === wrapper.current;
      setNativeFullscreen(ownsFullscreen);
      if (ownsFullscreen) setExpandedFallback(false);
    };
    document.addEventListener('fullscreenchange', changed);
    return () => { mounted.current = false; document.removeEventListener('fullscreenchange', changed); };
  }, []);
  useEffect(() => {
    if (wasExpanded.current && !expanded) maximizeButton.current?.focus({ preventScroll: true });
    if (!wasExpanded.current && expanded) maximizeButton.current?.focus({ preventScroll: true });
    wasExpanded.current = expanded;
    if (!expanded) return;
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && expandedFallback) { event.preventDefault(); setExpandedFallback(false); }
      if (event.key !== 'Tab') return;
      const controls = [...(wrapper.current?.querySelectorAll<HTMLElement>('button, a[href], [tabindex="0"]') ?? [])]
        .filter(element => !element.hasAttribute('disabled') && element.getClientRects().length && getComputedStyle(element).visibility !== 'hidden');
      const first = controls[0], last = controls[controls.length - 1];
      if (event.shiftKey && (document.activeElement === first || !wrapper.current?.contains(document.activeElement))) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && (document.activeElement === last || !wrapper.current?.contains(document.activeElement))) { event.preventDefault(); first?.focus(); }
    };
    document.addEventListener('keydown', keydown);
    return () => document.removeEventListener('keydown', keydown);
  }, [expanded, expandedFallback]);
  useEffect(() => {
    if (!expandedFallback) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { if (document.body.style.overflow === 'hidden') document.body.style.overflow = previous; };
  }, [expandedFallback]);
  const toggleFullscreen = async () => {
    if (fullscreenRequest.current) return;
    if (expandedFallback) { setExpandedFallback(false); return; }
    if (document.fullscreenElement === wrapper.current) {
      try { await document.exitFullscreen(); } catch { /* Keep the actual browser fullscreen state. */ }
      return;
    }
    fullscreenRequest.current = true;
    try {
      if (!wrapper.current?.requestFullscreen || !document.fullscreenEnabled) throw new Error('Fullscreen unavailable');
      await wrapper.current.requestFullscreen();
      if (mounted.current) setNativeFullscreen(document.fullscreenElement === wrapper.current);
    } catch { if (mounted.current) setExpandedFallback(true); }
    finally { fullscreenRequest.current = false; }
  };
  useEffect(() => {
    if (!container.current) return;
    let active = true;
    setFailed(false);
    let instance: maplibregl.Map;
    try {
      instance = new maplibregl.Map({ container: container.current, center: [0, 0], zoom: 0, minZoom: -2, maxZoom: 5,
        bearing: 0, pitch: 0, maxPitch: 0, dragRotate: false, touchPitch: false, trackResize: false, renderWorldCopies: false, attributionControl: false,
        style: { version: 8, sources: { world: { type: 'geojson', data: '/maps/world.geojson', attribution: 'Natural Earth · public domain' } }, layers: [
          { id: 'ocean', type: 'background', paint: { 'background-color': '#111914' } },
          { id: 'land', type: 'fill', source: 'world', paint: { 'fill-color': '#26392c', 'fill-opacity': .9 } },
          { id: 'borders', type: 'line', source: 'world', paint: { 'line-color': '#3c5140', 'line-width': .6 } },
        ] },
      });
      map.current = instance;
      instance.dragRotate.disable();
      instance.touchZoomRotate.disableRotation();
      instance.keyboard.disableRotation();
      instance.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-left');
      instance.addControl(new maplibregl.AttributionControl({ compact: false }), 'bottom-right');
      let visible = false, lastWidth = 0, lastHeight = 0, hasInitialView = false;
      const resizeViewport = (width: number, height: number) => {
        if (width <= 0 || height <= 0) { visible = false; return; }
        const dimensionsChanged = width !== lastWidth || height !== lastHeight;
        const becameVisible = !visible;
        if (!dimensionsChanged && !becameVisible) return;
        visible = true;
        const refit = !hasInitialView;
        resizeAtlasViewport(instance, ENDPOINT_BOUNDS, refit);
        if (refit) hasInitialView = true;
        lastWidth = width; lastHeight = height;
      };
      instance.on('load', () => {
        if (!active) return;
        setReady(true); setFailed(false);
        const { width, height } = container.current!.getBoundingClientRect();
        resizeViewport(width, height);
      });
      instance.on('error', () => { if (active && !instance.isStyleLoaded()) setFailed(true); });
      instance.getCanvas().addEventListener('webglcontextlost', () => { if (active) setFailed(true); });
      instance.getCanvas().addEventListener('webglcontextrestored', () => { if (active) setFailed(false); });
      const observer = new ResizeObserver(entries => {
        const { width, height } = entries[0].contentRect;
        resizeViewport(width, height);
      }); observer.observe(container.current);
      return () => { active = false; observer.disconnect(); instance.remove(); map.current = null; setReady(false); };
    } catch { setFailed(true); }
  }, []);
  useEffect(() => {
    if (!ready || !map.current) return;
    map.current.setPaintProperty('ocean', 'background-color', theme === 'light' ? '#e6ece4' : '#111914');
    map.current.setPaintProperty('land', 'fill-color', theme === 'light' ? '#c1cfba' : '#26392c');
    map.current.setPaintProperty('borders', 'line-color', theme === 'light' ? '#a1b397' : '#3c5140');
  }, [ready, theme]);
  useEffect(() => {
    if (!ready || !map.current || !origin) return;
    const element = document.createElement('div'); element.className = 'origin-marker'; element.textContent = '+'; element.title = 'Your configured origin location';
    const marker = new maplibregl.Marker({ element }).setLngLat(origin).addTo(map.current);
    return () => { marker.remove(); };
  }, [ready, origin?.[0], origin?.[1]]);
  return <div ref={wrapper} style={{ '--atlas-ratio': MAP_ASPECT_RATIO } as CSSProperties} className={`atlas-wrap${frameWidth > 0 && frameWidth < 800 ? ' atlas-compact' : ''}${expanded ? ' atlas-expanded' : ''}${expandedFallback ? ' atlas-expanded-fallback' : ''}`} role={expanded ? 'dialog' : undefined} aria-modal={expanded ? true : undefined} aria-label={expanded ? 'Endpoint atlas' : undefined}>
    <div className="map-heading"><div><span className="eyebrow">01 / THE ATLAS</span><h2>A world of endpoints.</h2></div><div className="map-heading-actions"><p>{targets.filter(t => t.geography.coordinates).length} mapped</p><button ref={maximizeButton} className="atlas-maximize" type="button" aria-expanded={expanded} onClick={() => void toggleFullscreen()}>{expanded ? 'Exit full screen' : 'Full screen'}</button></div></div>
    <div className={failed ? 'atlas-frame failed' : 'atlas-frame'} style={!expanded && frameWidth >= 800 ? { height: Math.max(300, frameWidth / MAP_ASPECT_RATIO) } : undefined}><div className="atlas-canvas" style={{ aspectRatio: MAP_ASPECT_RATIO }}><div ref={container} className="atlas" aria-label="Map of approximate endpoint regions. Select a marker to inspect measurements. Browse every target in the signal directory." /></div>
      {ready && !failed && map.current && <ProbeAtlas map={map.current} targets={targets} registeredTargetIds={registeredTargetIds} selected={selected} onSelect={onSelect} family={family} now={now} arrivals={arrivals} motionEnabled={motionEnabled} theme={theme} origin={origin} accessAllowanceMS={accessAllowanceMS} />}
      {failed && <div className="map-fallback"><span className="orb-icon">◎</span><h3>Explore through the directory</h3><p>Map view unavailable. Use the directory to explore targets and measurements.</p><a href="#directory">Open signal directory ↘</a></div>}
    </div>
    <div className="map-footer"><div className="map-key"><div className="progressive-legend">
      <span><i className="baseline-gradient"/>Latency vs distance reference</span>
      <span><i className="legend-loss-bar"/>Bar: past-hour loss or failures</span>
      <span><i className="legend-status-pattern"/>Patterned row: hover or select for status</span>
      <span><i className="legend-ripple"/>Recent result</span>
      <span>+ = configured local origin</span><span>I = ICMP · U = UDP</span>
    </div><details className="map-legend-help"><summary>How to read the map</summary>
      <p>Colour compares fresh ICMP or UDP timing with an approximate distance reference from your configured local origin: green up to 150%, red from 300%. The reference is max(10 ms, distance in km x 0.01 + 10 ms), a display heuristic rather than a latency prediction or health test. Other methods, missing origin/locations and stale timings use a neutral colour.</p>
      <p>The bar shows past-hour packet loss or failed attempts; only positive rates have a bar, and its fill keeps the original precision. Small positive rates remain visible in row help. An unavailable rate remains unknown.</p>
      <p>Diagonal amber lines mark degraded timing or loss. Blue-gray dots mark stale, unknown, unverified or unavailable results. Rose horizontal lines mark a measured network failure; local inability to measure uses information dots. Hover, focus or select an individual row for its reason.</p>
      <p>Elevated timing requires a fresh result and a fresh, positive 24-hour baseline, with both at least 50% and at least 10 milliseconds increase. Pulses highlight newly completed local results; saved history is never replayed as a new arrival. ICMP, UDP, TCP, HTTPS and DNS identify the probe method.</p>
    </details></div>
      <button className="motion-toggle" type="button" aria-pressed={motionEnabled} onClick={() => onMotionEnabledChange(!motionEnabled)}>Arrival motion {motionEnabled ? 'on' : 'off'}</button>

    </div>
  </div>;
}
