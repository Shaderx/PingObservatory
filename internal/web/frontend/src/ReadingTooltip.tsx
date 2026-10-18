import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

export function ReadingTooltip({ id, anchor, lines, onDismiss, onEnter, onLeave }: {
  id: string; anchor: HTMLElement; lines: string[]; onDismiss(): void; onEnter(): void; onLeave(): void;
}) {
  const element = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ left: 12, top: 12 });
  useLayoutEffect(() => {
    const place = () => {
      const box = anchor.getBoundingClientRect(), tip = element.current?.getBoundingClientRect();
      if (!tip || !anchor.isConnected) { onDismiss(); return; }
      const left = Math.max(12, Math.min(innerWidth - tip.width - 12, box.right - tip.width));
      const below = box.bottom + 6;
      const preferredTop = below + tip.height <= innerHeight - 12 ? below : box.top - tip.height - 6;
      const top = Math.max(12, Math.min(innerHeight - tip.height - 12, preferredTop));
      setPosition({ left, top });
    };
    place();
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); };
  }, [anchor, lines.join('\n'), onDismiss]);
  useEffect(() => {
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.stopPropagation(); onDismiss(); } };
    const outside = (event: PointerEvent) => {
      const target = event.target as Node | null;
      if (target && !anchor.contains(target) && !element.current?.contains(target)) onDismiss();
    };
    document.addEventListener('keydown', key, true);
    document.addEventListener('pointerdown', outside, true);
    return () => { document.removeEventListener('keydown', key, true); document.removeEventListener('pointerdown', outside, true); };
  }, [anchor, onDismiss]);
  return createPortal(<div ref={element} id={id} role="tooltip" className="map-reading-tooltip" style={position}
    onMouseEnter={onEnter} onMouseLeave={onLeave}>
    {lines.map((line, index) => <p key={index}>{line}</p>)}
  </div>, document.fullscreenElement ?? document.body);
}
