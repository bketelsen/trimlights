import { useEffect, useState } from 'react';
import { request, SignedOut } from './api';
import { latestRequests } from './latest';
import { planSummary, type PlanSummary } from './homePlan';
import type { SavedPattern } from './pattern';
import type { Plan, Preview } from './Schedule';

// loading: nothing has answered yet. failed: the last request failed; patterns keeps
// the last list, so favorites stay on screen while the plan says it could not load.
export type HomePlanData = {
  state: 'loading' | 'ready' | 'failed';
  patterns: SavedPattern[];
  summary: PlanSummary | null;
  timezone: string;
};
const empty: HomePlanData = { state: 'loading', patterns: [], summary: null, timezone: '' };
const week = 7 * 24 * 60 * 60 * 1000;

async function fetchHomePlan(csrf: string): Promise<HomePlanData> {
  const [saved, library] = await Promise.all([
    request<{ plan: Plan }>('/api/v1/schedule'),
    request<{ patterns: SavedPattern[] }>('/api/v1/patterns?include_archived=1'),
  ]);
  const from = new Date();
  // The preview evaluates the saved calendar only. It reads no controller state and
  // sends nothing to the lights.
  const preview = await request<Preview>('/api/v1/schedule/preview', csrf, {
    plan: saved.plan,
    from: from.toISOString(),
    until: new Date(from.getTime() + week).toISOString(),
  });
  return {
    state: 'ready',
    patterns: library.patterns,
    summary: planSummary(preview, library.patterns),
    timezone: preview.timezone,
  };
}

// useHomePlan loads the saved calendar and library while Home is on screen, and again
// every minute so the "now" step moves on at a boundary. onSignedOut receives only a
// SignedOut error; any other failure stays in the card.
export function useHomePlan(
  active: boolean,
  csrf: string,
  onSignedOut: (error: unknown) => void,
): HomePlanData {
  const [data, setData] = useState(empty);
  const [requests] = useState(latestRequests);
  useEffect(() => {
    if (!active) return;
    let live = true;
    const run = () => {
      const settle = requests.begin();
      fetchHomePlan(csrf).then(
        (result) => {
          if (settle() && live) setData(result);
        },
        (error: unknown) => {
          if (!settle() || !live) return;
          if (error instanceof SignedOut) onSignedOut(error);
          else setData((last) => ({ ...last, state: 'failed' }));
        },
      );
    };
    run();
    const timer = setInterval(() => {
      if (!document.hidden && !requests.busy(60000)) run();
    }, 60000);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [active, csrf, onSignedOut, requests]);
  return data;
}

function planTime(value: string, timezone: string) {
  const format = (timeZone?: string) =>
    new Date(value).toLocaleString(undefined, {
      timeZone,
      weekday: 'short',
      hour: 'numeric',
      minute: '2-digit',
    });
  try {
    return format(timezone || undefined);
  } catch {
    // A browser that does not know the calendar's time zone shows its own local time.
    return format();
  }
}

export function HomePlanCard({
  data,
  onOpenCalendar,
}: {
  data: HomePlanData;
  onOpenCalendar: () => void;
}) {
  const { state, summary, timezone } = data;
  return (
    <section className="panel home-plan" aria-labelledby="home-plan-title">
      <div className="row">
        <div>
          <span className="eyebrow">FROM YOUR CALENDAR</span>
          <h2 id="home-plan-title">Planned next</h2>
        </div>
        <span className="status-pill">Plan only</span>
      </div>
      {state === 'loading' && <p className="muted">Loading your plan…</p>}
      {state === 'failed' && (
        <p className="notice">We couldn’t load your calendar plan. It will try again shortly.</p>
      )}
      {state === 'ready' &&
        (summary ? (
          <dl className="plan-steps">
            <div>
              <dt>Now</dt>
              <dd>
                <strong>{summary.now.label}</strong>
                <span>
                  {summary.now.detail}
                  {summary.next && ` · until ${planTime(summary.now.until, timezone)}`}
                </span>
              </dd>
            </div>
            {summary.next ? (
              <div>
                <dt>
                  <time dateTime={summary.next.from}>{planTime(summary.next.from, timezone)}</time>
                </dt>
                <dd>
                  <strong>{summary.next.label}</strong>
                  <span>{summary.next.detail}</span>
                </dd>
              </div>
            ) : (
              <div>
                <dt>Next</dt>
                <dd>
                  <span>No change is planned in the next 7 days.</span>
                </dd>
              </div>
            )}
          </dl>
        ) : (
          <p>
            Nothing is planned for the next 7 days. Add routines in Calendar to see what comes next.
          </p>
        ))}
      <p className="footnote">
        This is your saved plan, not what the lights are doing now. The lights follow their own
        timers until automatic playback is ready.
      </p>
      <div className="card-actions">
        <button className="button secondary" onClick={onOpenCalendar}>
          Open calendar
        </button>
      </div>
    </section>
  );
}
