import { useEffect, useRef, useState } from 'react';
import type { PointerEvent as ReactPointerEvent } from 'react';
import { canvasTouchAction, positions, sourceRun } from './house';
import type { CanvasMode, Layout, Photo, Point, View } from './house';
import { renderPattern } from './pattern';
import type { Design } from './pattern';
import { PreviewClock } from './previewClock';

type Props = {
  photo: Photo;
  view: View;
  layout: Layout;
  design?: Design;
  selectedRun?: string;
  mode?: CanvasMode;
  onPoint?: (point: Point) => void;
  onMove?: (index: number, point: Point, bulb: boolean) => void;
  onStart?: () => void;
  onSelect?: (index: number) => void;
};
export function HouseCanvas({
  photo,
  view,
  layout,
  design,
  selectedRun,
  mode = 'pan',
  onPoint,
  onMove,
  onStart,
  onSelect,
}: Props) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [picture, setPicture] = useState<HTMLImageElement>();
  const [imageError, setImageError] = useState(false);
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState<Point>({ x: 0, y: 0 });
  const [numbers, setNumbers] = useState(false);
  const [playing, setPlaying] = useState(
    () => !window.matchMedia('(prefers-reduced-motion: reduce)').matches,
  );
  const clock = useRef(new PreviewClock());
  const drag = useRef<{ pointer: number; index: number; start: Point; pan: Point } | null>(null);
  const width = view.turns % 2 ? photo.height : photo.width,
    height = view.turns % 2 ? photo.width : photo.height;
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- resets view state when the input changes; a keyed remount changes behaviour.
    setImageError(false);
    setPicture(undefined);
    setZoom(1);
    setPan({ x: 0, y: 0 });
    let live = true;
    const img = new Image();
    img.onload = () => {
      if (live) setPicture(img);
    };
    img.onerror = () => {
      if (live) setImageError(true);
    };
    img.src = `/api/v1/photos/${photo.id}/image`;
    return () => {
      live = false;
    };
  }, [photo.id]);
  useEffect(() => {
    const el = canvas.current;
    if (!el) return;
    const ctx = el.getContext('2d');
    if (!ctx) return;
    const resize = () => {
      const rect = el.getBoundingClientRect(),
        dpr = Math.min(window.devicePixelRatio || 1, 2);
      el.width = Math.max(1, Math.round(rect.width * dpr));
      el.height = Math.max(1, Math.round(rect.height * dpr));
    };
    resize();
    let frame = 0;
    clock.current.restart();
    function draw(now: number) {
      if (!el || !ctx) return;
      const w = el.width,
        h = el.height;
      ctx.setTransform(1, 0, 0, 1, 0, 0);
      ctx.fillStyle = '#09121b';
      ctx.fillRect(0, 0, w, h);
      ctx.save();
      ctx.translate(w / 2, h / 2);
      ctx.scale(zoom, zoom);
      ctx.translate(-w / 2 + pan.x * w, -h / 2 + pan.y * h);
      if (picture) {
        ctx.save();
        ctx.translate(w / 2, h / 2);
        ctx.rotate((view.turns * Math.PI) / 2);
        ctx.drawImage(
          picture,
          -(view.turns % 2 ? h : w) / 2,
          -(view.turns % 2 ? w : h) / 2,
          view.turns % 2 ? h : w,
          view.turns % 2 ? w : h,
        );
        ctx.restore();
      }
      const tick = clock.current.tick(
        now,
        playing && design?.effect === 'chase' && !document.hidden,
      );
      let colors: string[] = [];
      if (design && design.effect !== 'native') colors = renderPattern(design, 2048, tick);
      const dot = Math.max(2, Math.min(w, h) * 0.005) / zoom;
      for (const mapping of view.mappings) {
        const run = layout.runs.find((r) => r.id === mapping.run_id);
        if (!run) continue;
        const selected = run.id === selectedRun;
        const bulbs = positions(mapping, run.count, width, height),
          source = sourceRun(run, layout.runs);
        if (selected) {
          ctx.beginPath();
          mapping.points.forEach((p, i) => {
            if (i === 0) ctx.moveTo(p.x * w, p.y * h);
            else ctx.lineTo(p.x * w, p.y * h);
          });
          ctx.strokeStyle = '#d4efab';
          ctx.lineWidth = 2 / zoom;
          ctx.stroke();
        }
        bulbs.forEach((p, i) => {
          ctx.beginPath();
          ctx.arc(p.x * w, p.y * h, dot, 0, 2 * Math.PI);
          ctx.fillStyle = colors[source.start - 1 + i] ?? '#d4efab';
          ctx.shadowColor = ctx.fillStyle;
          ctx.shadowBlur = colors.length ? dot * 3 : 0;
          ctx.fill();
          ctx.shadowBlur = 0;
          if (numbers && (zoom >= 2 || run.count <= 60)) {
            ctx.font = `${Math.max(10, w * 0.012) / zoom}px sans-serif`;
            ctx.fillStyle = 'white';
            ctx.strokeStyle = '#111';
            ctx.lineWidth = 3 / zoom;
            const label = String(run.start + i);
            ctx.strokeText(label, p.x * w + dot, p.y * h - dot);
            ctx.fillText(label, p.x * w + dot, p.y * h - dot);
          }
        });
        if (selected && mode === 'points') {
          mapping.points.forEach((p) => {
            ctx.fillStyle = '#fff';
            ctx.strokeStyle = '#253d24';
            ctx.lineWidth = 2 / zoom;
            ctx.beginPath();
            ctx.arc(p.x * w, p.y * h, dot * 2.5, 0, 2 * Math.PI);
            ctx.fill();
            ctx.stroke();
          });
        }
      }
      ctx.restore();
      if (playing && design?.effect === 'chase' && design.speed > 0 && !document.hidden)
        frame = requestAnimationFrame(draw);
    }
    const observer = new ResizeObserver(() => {
      resize();
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(draw);
    });
    observer.observe(el);
    const resume = () => {
      cancelAnimationFrame(frame);
      clock.current.restart();
      frame = requestAnimationFrame(draw);
    };
    document.addEventListener('visibilitychange', resume);
    frame = requestAnimationFrame(draw);
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      document.removeEventListener('visibilitychange', resume);
    };
  }, [
    picture,
    view,
    layout,
    design,
    selectedRun,
    mode,
    width,
    height,
    zoom,
    pan,
    numbers,
    playing,
  ]);
  function point(event: ReactPointerEvent<HTMLCanvasElement>): Point {
    const rect = event.currentTarget.getBoundingClientRect();
    return {
      x: ((event.clientX - rect.left) / rect.width - 0.5) / zoom + 0.5 - pan.x,
      y: ((event.clientY - rect.top) / rect.height - 0.5) / zoom + 0.5 - pan.y,
    };
  }
  function down(event: ReactPointerEvent<HTMLCanvasElement>) {
    if (drag.current) return;
    const p = point(event),
      mapping = view.mappings.find((m) => m.run_id === selectedRun),
      run = layout.runs.find((r) => r.id === selectedRun);
    if (mode === 'draw') {
      if (p.x >= 0 && p.x <= 1 && p.y >= 0 && p.y <= 1) onPoint?.(p);
      return;
    }
    let index = -1;
    if (mode !== 'pan' && mapping && run) {
      const rect = event.currentTarget.getBoundingClientRect();
      const options =
        mode === 'bulbs' ? positions(mapping, run.count, width, height) : mapping.points;
      let closest = 28;
      options.forEach((v, i) => {
        const distance = Math.hypot(
          (v.x - p.x) * rect.width * zoom,
          (v.y - p.y) * rect.height * zoom,
        );
        if (distance < closest) {
          closest = distance;
          index = i;
        }
      });
      if (index < 0) return;
      onStart?.();
      onSelect?.(index);
    } else if (mode !== 'pan') return;
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = {
      pointer: event.pointerId,
      index,
      start: { x: event.clientX, y: event.clientY },
      pan,
    };
  }
  function move(event: ReactPointerEvent<HTMLCanvasElement>) {
    const d = drag.current;
    if (!d || d.pointer !== event.pointerId) return;
    if (mode === 'pan') {
      const rect = event.currentTarget.getBoundingClientRect();
      setPan({
        x: d.pan.x + (event.clientX - d.start.x) / rect.width / zoom,
        y: d.pan.y + (event.clientY - d.start.y) / rect.height / zoom,
      });
      return;
    }
    const p = point(event);
    onMove?.(
      d.index,
      { x: Math.max(0, Math.min(1, p.x)), y: Math.max(0, Math.min(1, p.y)) },
      mode === 'bulbs',
    );
  }
  function end(event: ReactPointerEvent<HTMLCanvasElement>) {
    if (drag.current?.pointer === event.pointerId) drag.current = null;
  }
  return (
    <div className="house-canvas-wrap">
      {imageError && (
        <p role="alert">Photo could not load. Check your connection or sign in again.</p>
      )}
      <canvas
        ref={canvas}
        className={`house-canvas${canvasTouchAction(mode, zoom) === 'none' ? ' gesture-locked' : ''}`}
        style={{ aspectRatio: `${width}/${height}` }}
        aria-label="House photo with mapped roofline bulbs. Use Fine-tune corners and Fine-tune bulbs for keyboard editing."
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={end}
        onPointerCancel={end}
        onLostPointerCapture={end}
      />
      <div className="house-toolbar">
        <button
          type="button"
          className="button secondary"
          onClick={() => setZoom((z) => Math.max(1, z / 1.5))}
          aria-label="Zoom out"
        >
          −
        </button>
        <span>{Math.round(zoom * 100)}%</span>
        <button
          type="button"
          className="button secondary"
          onClick={() => setZoom((z) => Math.min(8, z * 1.5))}
          aria-label="Zoom in"
        >
          +
        </button>
        <button
          type="button"
          className="button secondary"
          onClick={() => {
            setZoom(1);
            setPan({ x: 0, y: 0 });
          }}
        >
          Fit photo
        </button>
        <label>
          <input type="checkbox" checked={numbers} onChange={(e) => setNumbers(e.target.checked)} />{' '}
          Numbers
        </label>
      </div>
      {design?.effect === 'chase' && (
        <button
          type="button"
          className="button secondary"
          disabled={design.speed === 0}
          onClick={() => setPlaying((p) => !p)}
        >
          {playing && design.speed > 0 ? 'Pause animation' : 'Play animation'}
        </button>
      )}
      {design?.effect === 'chase' && design.speed === 0 && (
        <p className="muted">Increase the speed to see your colors move.</p>
      )}
      <p className="muted">
        {design?.effect === 'native'
          ? 'A color preview isn’t available for this saved pattern yet.'
          : design
            ? 'Design preview'
            : 'Choose a pattern to try its colors on your house.'}
      </p>
    </div>
  );
}
