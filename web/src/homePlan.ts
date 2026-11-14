// Home reads two things from saved household data without contacting the lights:
// what the saved calendar plans now and next, and which favorites are saved patterns
// on the lights. Both are pure functions of cached answers, so tests need no browser.
import type { SavedPattern } from './pattern';
import type { Action, Preview } from './Schedule';

type Segment = Preview['segments'][number];
export type PlannedStep = { label: string; detail: string; from: string; until: string };
export type PlanSummary = { now: PlannedStep; next?: PlannedStep };
// name is the Library name; saved is the name the lights hold for the same slot, which
// is what the pattern list and the "Playing …" notice show.
export type Favorite = { slot: number; name: string; saved: string };
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

// sameLights is true when two steps ask the same of the lights. The evaluator starts a
// new segment whenever the winning or shadowed routines change, so adjacent segments
// can differ only in the routine behind an unchanged action.
function sameLights(a: Segment, b: Segment) {
  return (
    a.conflict === b.conflict &&
    a.action.kind === b.action.kind &&
    a.action.pattern_id === b.action.pattern_id &&
    a.action.revision === b.action.revision
  );
}

// planSummary returns the current planned step and the next step that changes the
// lights, or null when the saved calendar plans nothing in the previewed range (every
// step is the default "off" with no routine behind it). "Now" lasts until that change,
// not until the first segment ends.
export function planSummary(preview: Preview, patterns: SavedPattern[]): PlanSummary | null {
  const segments = preview.segments;
  if (!segments.length || segments.every((s) => !s.winners.length && !s.conflict)) return null;
  const name = (action: Action) =>
    action.kind === 'off'
      ? 'Lights off'
      : (patterns.find((p) => p.id === action.pattern_id)?.design.name ?? 'Saved pattern');
  const change = segments.find((s) => !sameLights(segments[0], s));
  const now = step(segments[0], name);
  return change ? { now: { ...now, until: change.from }, next: step(change, name) } : { now };
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
    const saved = slot.name || `Pattern ${native.id}`;
    out.push({ slot: native.id, name: p.design.name || saved, saved });
  }
  return out;
}
