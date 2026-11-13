// Home reads two things from saved household data without contacting the lights:
// what the saved calendar plans now and next, and which favorites are saved patterns
// on the lights. Both are pure functions of cached answers, so tests need no browser.
import type { SavedPattern } from './pattern';
import type { Action, Preview } from './Schedule';

type Segment = Preview['segments'][number];
export type PlannedStep = { label: string; detail: string; from: string; until: string };
export type PlanSummary = { now: PlannedStep; next?: PlannedStep };
export type Favorite = { slot: number; name: string };
type SnapshotPattern = { id: number; name: string; uninterpreted_parameters_hex?: string };

function step(segment: Segment, name: (action: Action) => string): PlannedStep {
  return {
    label: segment.conflict ? 'Overlapping routines need a choice' : name(segment.action),
    detail: segment.winners.length
      ? segment.winners.map((w) => w.name).join(' + ')
      : 'Between routines',
    from: segment.from,
    until: segment.until,
  };
}

// planSummary returns the current planned step and the next different one, or null
// when the saved calendar plans nothing in the previewed range (every step is the
// default "off" with no routine behind it).
export function planSummary(preview: Preview, patterns: SavedPattern[]): PlanSummary | null {
  const segments = preview.segments;
  if (!segments.length || segments.every((s) => !s.winners.length && !s.conflict)) return null;
  const name = (action: Action) =>
    action.kind === 'off'
      ? 'Lights off'
      : (patterns.find((p) => p.id === action.pattern_id)?.design.name ?? 'Saved pattern');
  return {
    now: step(segments[0], name),
    ...(segments[1] ? { next: step(segments[1], name) } : {}),
  };
}

// favoriteShortcuts lists the favorites that Home can offer for playing: a favorite
// qualifies only when it was imported from the lights and the slot it came from still
// holds the same parameters in the cached snapshot. A favorite that the app designed,
// or whose slot has changed since the import, is left out instead of guessed.
export function favoriteShortcuts(
  patterns: SavedPattern[],
  snapshot?: { patterns: SnapshotPattern[] },
): Favorite[] {
  if (!snapshot) return [];
  const out: Favorite[] = [];
  for (const p of patterns) {
    const native = p.design.native;
    if (p.archived || !p.design.favorite || p.design.effect !== 'native' || !native) continue;
    const slot = snapshot.patterns.find((s) => s.id === native.id);
    if (
      !slot?.uninterpreted_parameters_hex ||
      slot.uninterpreted_parameters_hex.toLowerCase() !== native.parameters_hex.toLowerCase() ||
      out.some((f) => f.slot === native.id)
    )
      continue;
    out.push({ slot: native.id, name: p.design.name || slot.name || `Pattern ${native.id}` });
  }
  return out;
}
