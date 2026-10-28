# Spec: Household calendar and intent preview

This contract governs saved calendar plans and the pure Go evaluator. Hardware
execution is unavailable: every preview and saved rule MUST identify this limit.

## Interface

The versioned plan has `schema: 1`, an IANA `timezone`, optional `location`
(`latitude`, `longitude`), `rules`, and optional `override`.

| Field | Type | Constraints |
| --- | --- | --- |
| Rule `id`, `name` | strings | Unique stable ID; nonempty trimmed name, at most 80 characters |
| `enabled`, `end_next_day` | booleans | Next-day is explicit, never inferred |
| `kind` | string | `weekly`, `seasonal`, `exception` |
| `priority` | integer | 0–100; higher wins within a kind |
| `from`, `through` | local dates | Both required for seasonal/exception, optional paired weekly bounds; inclusive |
| `weekdays` | integer array | 0 Sunday through 6 Saturday; nonempty unique, ignored for exception (must be empty) |
| `start`, `end` | boundary | `kind: clock` plus `time: HH:MM`, or `sunrise`/`sunset` plus `offset_minutes: -180..180` |
| `action` | object | `kind: off`, or `kind: pattern` with `pattern_id` and positive `revision` |
| Override | object | `id`, action, RFC3339 `starts_at`, `expires_at`; positive duration ≤ 7 days |

At most 100 rules are accepted. Dates are Gregorian 1970–2199. Exception dates
must match. Solar boundaries require a configured finite location in geographic
bounds. Timezones are embedded in the Go binary for scratch-container deployment.

- `GET /api/v1/schedule` returns `{revision, plan, actor, updated}` and ETag.
  An untouched calendar is revision 1, America/New_York, empty rules, preview only.
- `PUT /api/v1/schedule` accepts a plan with `If-Match: "N"`; missing precondition
  is 428, stale revision 409, invalid plan/reference 400. Body limit is 128 KiB.
- `GET /api/v1/schedule/revisions` returns retained revisions, newest first.
- `POST /api/v1/schedule/preview` accepts `{plan, from, until}`; range is positive
  and at most 31 elapsed days. Alternatively `{plan, from_date, days}` accepts a
  local `YYYY-MM-DD` start date and 1–30 civil days; it resolves both midnights in
  the household timezone. Mixed forms are rejected. Neither form persists or
  accesses a controller.
- `GET /api/v1/patterns/{id}/schedule-references` lists current calendar rule and
  override references, including disabled rules and pinned versions.

## Rules

1. Dates and weekday selection refer to the start's civil date before solar
   offsets. Overnight end boundaries use the following civil date. Intervals are
   half-open. A start equals an end only for explicit next-day full-day rules.
2. A spring DST gap shifts the requested time forward by that gap. A repeated
   start uses the earlier instant; a repeated end uses the later instant. A whole
   skipped civil date is not translated to a different date. Adjustments and
   skipped occurrences appear in preview issues.
3. Missing sunrise/sunset skips that occurrence with an issue, never fabricates a
   clock fallback. Solar times are local astronomical estimates from pinned
   [go-sunrise v1.1.0](https://github.com/nathan-osman/go-sunrise/tree/v1.1.0),
   rounded to the nearest minute.
4. Precedence is override > exception > seasonal > weekly > off. Highest numeric
   priority within a kind wins. Ties with the same action agree; ties with different
   actions return off with `conflict: true` and name the conflicting rules.
5. Preview covers the complete requested interval, including default-off gaps,
   winners and shadowed rules, in deterministic order. Re-evaluation of any instant
   after restart MUST produce the same current intent as a continuous timeline.
6. Each save creates an immutable revision. All referenced pattern revisions must
   exist. A newly introduced reference must point to an unarchived pattern and an
   unarchived revision. Existing references survive later edits and archives.
7. No pattern revision is deleted. Editing a referenced pattern does not alter the
   pinned plan. The pattern UI MUST explain references before edits/archives.
8. Persist override expiry as an absolute instant. Clearing the override resumes
   the calendar in preview. Runtime execution, ownership handoff and delivery
   verification remain separate outstanding requirements.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| Calendar timeline | Go evaluator, shared by preview and future worker reconciliation |
| Revision and reference tables | Saved plan committed in one SQLite transaction |

## References

- Rationale: [ADR-0006](../adr/0006-explicit-calendar-intent.md).
- Context: [application design](../design/application.md).
- Delivery: [roadmap](../plans/roadmap.md), [v1 acceptance](v1-acceptance.md).
