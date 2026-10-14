import { useEffect, useRef, useState } from 'react';
import * as maplibregl from 'maplibre-gl';
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url';
import 'maplibre-gl/dist/maplibre-gl.css';
import type { Family, Point, Target } from './types';
import { ENDPOINT_BOUNDS, MAP_ASPECT_RATIO } from './map-viewport';
import { ProbeAtlas } from './ProbeAtlas';

maplibregl.setWorkerUrl(workerUrl);

export function Atlas({ targets, selected, onSelect, family, now, arrivals, motionEnabled, onMotionEnabledChange, theme, origin }: { targets: Target[]; selected: string; onSelect(id: string): void; family: Family; now: number; arrivals: ReadonlyMap<string, number>; motionEnabled: boolean; onMotionEnabledChange(enabled: boolean): void; theme: string; origin?: Point | null }) {
  const container = useRef<HTMLDivElement>(null), map = useRef<maplibregl.Map | null>(null);
  const [ready, setReady] = useState(false), [failed, setFailed] = useState(false);
  useEffect(() => {
    if (!container.current) return;
    let active = true;
    setFailed(false);
    try {
      const instance = new maplibregl.Map({ container: container.current, center: [0, 0], zoom: 0, minZoom: -2, maxZoom: 5,
        bearing: 0, pitch: 0, maxPitch: 0, dragRotate: false, touchPitch: false, trackResize: false, renderWorldCopies: false, attributionControl: false,
        style: { version: 8, sources: { world: { type: 'geojson', data: '/maps/world.geojson', attribution: 'Natural Earth / public domain' } }, layers: [
          { id: 'ocean', type: 'background', paint: { 'background-color': '#111914' } },
          { id: 'land', type: 'fill', source: 'world', paint: { 'fill-color': '#26392c', 'fill-opacity': .9 } },
          { id: 'borders', type: 'line', source: 'world', paint: { 'line-color': '#3c5140', 'line-width': .6 } },
        ] },
      });
      map.current = instance;
      instance.dragRotate.disable(); instance.touchZoomRotate.disableRotation(); instance.keyboard.disableRotation();
      instance.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-left');
      instance.addControl(new maplibregl.AttributionControl({ compact: false }), 'bottom-right');
      let visible = false, lastWidth = 0, lastHeight = 0, hasInitialView = false;
      const resizeViewport = (width: number, height: number) => {
        if (width <= 0 || height <= 0) { visible = false; return; }
        const dimensionsChanged = width !== lastWidth || height !== lastHeight;
        if (!dimensionsChanged && visible) return;
        visible = true; instance.resize();
        if (dimensionsChanged || !hasInitialView) { instance.fitBounds(ENDPOINT_BOUNDS, { padding: 0, duration: 0 }); hasInitialView = true; }
        lastWidth = width; lastHeight = height;
      };
      instance.on('load', () => { if (!active) return; setReady(true); setFailed(false); const { width, height } = container.current!.getBoundingClientRect(); resizeViewport(width, height); });
      instance.on('error', () => { if (active && !instance.isStyleLoaded()) setFailed(true); });
      instance.getCanvas().addEventListener('webglcontextlost', () => { if (active) setFailed(true); });
      instance.getCanvas().addEventListener('webglcontextrestored', () => { if (active) setFailed(false); });
      const observer = new ResizeObserver(entries => { const { width, height } = entries[0].contentRect; resizeViewport(width, height); }); observer.observe(container.current);
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
    const marker = new maplibregl.Marker({ element }).setLngLat([origin.longitude, origin.latitude]).addTo(map.current);
    return () => { marker.remove(); };
  }, [ready, origin?.latitude, origin?.longitude]);
  return <div className="atlas-wrap">
    <div className="map-heading"><div><span className="eyebrow">01 / THE ATLAS</span><h2>A world of endpoints.</h2></div><p>{targets.filter(target => target.geography.coordinates).length} mapped / approximate locations</p></div>
    <div className={failed ? 'atlas-frame failed' : 'atlas-frame'}><div className="atlas-canvas" style={{ aspectRatio: MAP_ASPECT_RATIO }}><div ref={container} className="atlas" aria-label="Map of approximate endpoint locations. Select a marker to inspect measurements. Browse every target in the signal directory." /></div>
      {ready && !failed && map.current && <ProbeAtlas map={map.current} targets={targets} selected={selected} onSelect={onSelect} family={family} now={now} arrivals={arrivals} motionEnabled={motionEnabled} theme={theme} />}
      {failed && <div className="map-fallback"><span className="orb-icon">+</span><h3>Explore through the directory</h3><p>Map view unavailable. Use the directory to explore targets and measurements.</p><a href="#directory">Open signal directory</a></div>}
    </div>
    <div className="map-footer"><div className="map-key"><div className="progressive-legend"><span><i className="legend-ripple"/>New completed result</span><span>Green = response / red = attempted failure / gray = N/A or stale</span><span>+ = your origin</span></div><p className="progressive-legend-note">N/A means no measurement was attempted. Timeout, unreachable and probe errors are recorded separately. Leader lines connect labels to configured approximate locations.</p></div>
      <button className="motion-toggle" type="button" aria-pressed={motionEnabled} onClick={() => onMotionEnabledChange(!motionEnabled)}>Arrival motion {motionEnabled ? 'on' : 'off'}</button>
      <div className="map-note">APPROXIMATE GEOGRAPHY<br/><span>Locations are supplied by the catalogue or you.</span></div>
    </div>
  </div>;
}
