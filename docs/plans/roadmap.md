# Plan: From controller prototype to household v1

Planning baseline: **2026-10-28**. Deliver a mobile-friendly app on the owner's
homelab that he and his wife can use for shared lighting controls, pattern editing,
full calendar scheduling, and house-photo previews with manually mapped rooflines.
Use Go, frostyard/clix, the agentic-template structure, existing Caddy, and Pocket
ID. The [design](../design/application.md) separates what exists from the proposed
target; [acceptance](../specs/v1-acceptance.md) defines what counts as done.

Sizes are relative engineering effort (M/L), not calendar promises. Protocol work
is the main uncertainty. Scheduling is the first household beta; photo mapping is
part of completed v1, not an indefinitely deferred enhancement.

## Starting point — Proven prototype, not a production service

- [x] Capture local protocol and reproduce all 22 encoded messages in Go tests.
- [x] Read 18 patterns and both native daily schedules with the Go binary.
- [x] Verify a disabled-schedule change/restore on hardware using Python; port the
  bounded experiment to Go and test it with a simulated controller.
- [x] Build static amd64/arm64 binaries and a minimal container recipe.
- [ ] Remove fixed layout assumptions, verify Sunday clock encoding and ordinary
  controls, and test Go writes on hardware as part of a bounded integration pass.
- [ ] Add service ownership, storage, login, API, scheduler, frontend and deployment.

## Phase 1 — Repository foundation and clix (M)

Implements [application boundaries](../design/application.md#application-boundaries)
and [CLI acceptance](../specs/v1-acceptance.md#interface).

- Retrofit [agentic-template](https://github.com/bketelsen/agentic-template) in
  place. Preserve Go code, captures and experiments; do not recreate the repository
  from a template and discard the research. Initialize Git if still absent; choose
  repository owner/name before adding a remote or publishing.
- Add canonical AGENTS.md, with CLAUDE.md and GEMINI.md symlinks, plus
  `.github/copilot-instructions.md -> ../AGENTS.md` and
  `.claude/skills -> ../.agents/skills`. Keep only implemented code conventions in
  AGENTS.md. Future architecture stays in designs/plans.
- Adopt the template's ADR-0001/0002 with the actual adoption date, review ADR-0003,
  import remaining templates and enforce reciprocal links/document indexing.
  Create repeatable skills for protocol capture, bounded hardware validation and
  homelab release/restore as those procedures stabilize.
- Create a non-hardware `make check` used identically by CI: formatting check,
  vet/lint, unit/race/protocol tests, docs links and build. Keep hardware experiments
  explicitly separate. Organize Python under research when links are migrated;
  production builds/tests must not require it.
- Replace the ad-hoc flag CLI with clix/Cobra. Start from **clix v0.5.0**, verify
  the release when implementing, and pin it. Raise Go minimum/toolchain consistently
  with clix's Go 1.26.6/toolchain 1.26.10 requirements; the currently installed
  Go 1.27.3 can build that dependency. Carry version/commit/date/builder metadata.
- Preserve clix's reserved flags. Specify JSON envelopes and cancellation behavior;
  make dry-run perform no hidden clock sync. Convert CLI globals into configuration
  before invoking concurrent application code. Update the stdlib-only app claim.
- **Done when:** a fresh checkout passes `make check`, builds without Python, and
  the clix CLI passes help/version/JSON/error/dry-run regression checks; all agent
  entry points resolve to the same canonical instructions.

## Phase 2 — Reliable controller service (L, highest uncertainty)

Implements [worker design](../design/application.md#application-boundaries) and
[control rules](../specs/v1-acceptance.md#rules).

- Replace hardcoded schedule offsets with bounded parsing of variable records;
  preserve unknown fields and fail visibly on unsupported firmware/layouts.
- Verify clock/weekday encoding, off/manual/timer transitions, brightness, pattern
  playback, supported custom patterns, slot limits and output topology. Reuse
  existing captures first. Keep a capability matrix of observed vs inferred behavior.
- Run a narrow Go hardware integration test with baseline, one reversible edit,
  restoration and independent read-back. Avoid repeated physical tests once the
  behavior is established; use fixtures and a simulated controller for development.
- Build one long-running paced worker, operation IDs, deadlines, bounded recovery,
  state freshness and verified writes. Detect competing app sessions where possible.
  Make the normal CLI call the service instead of opening another hardware session.
- Add `serve`, configuration, typed application services, SQLite migrations and
  basic health/diagnostic endpoints. Separate readiness from controller availability.
- **Done when:** the service reads fresh state and handles control operations through
  one worker; simulated failures and one controlled reconnect recover without
  duplicate commands or unintended changes. Unverified features are unavailable
  with an explanation rather than guessed.

## Phase 3 — Shared mobile controls and pattern library (M–L)

Implements [household authentication](../design/application.md#household-state-and-authentication),
[pattern design](../design/application.md#pattern-library-and-photo-mapping), and
[Home/Patterns acceptance](../specs/v1-acceptance.md#interface).

- Embed a responsive TypeScript/React frontend in Go. Build Home, Patterns and
  Settings, with prominent scene/brightness/off/override/resume actions and
  controller freshness. Establish large touch targets and accessible navigation.
- Register a confidential Pocket ID OIDC client and exact callback at the chosen
  HTTPS hostname. Authenticate in Go, set secure sessions and CSRF protection,
  and allow the two household identities. Define ordinary vs administrative actions.
- Import the controller library into a versioned application pattern model. Add
  favorites, duplicate/edit, palette runs, supported effect parameters, import/export,
  a simple strip preview and explicit Apply. Preserve controller IDs and slot limits.
- Add optimistic concurrency, activity history and live UI updates across phones.
  Keep local previews separate from hardware actions.
- Deploy an early container through Caddy on one selected homelab host. Keep the
  service's future scheduler inactive while controller-native schedules still own
  the evenings.
- **Done when:** both users can sign in on their phones, see the same library,
  apply a verified scene and see the other phone update; concurrent edits cannot
  silently overwrite each other, and a local preview leaves the real lights alone.

## Phase 4 — Full scheduling and first household beta (L)

Implements [scheduling ownership](../design/application.md#scheduling-and-control-ownership)
and [schedule acceptance](../specs/v1-acceptance.md#rules).

- Specify and test weekly/date-range rules, one-off exceptions, sunset/sunrise
  offsets, overnight windows, timezone/DST behavior, priorities and conflicts.
  Use a fake clock and table-driven cases; do not wait for holidays to test them.
- Implement calendar editing plus a computed upcoming timeline: what runs, when,
  and why. Add duplicate seasonal rules, skip tonight, temporary overrides and
  resume schedule. Track scene revisions and impacts of editing/deleting them.
- Persist desired state and override expiry. On restart/reconnect, converge on the
  current rule result rather than replaying missed actions. Reconcile ambiguous
  writes through observed state. Surface external manufacturer-app changes.
- Migrate existing daily schedules into server-owned rules after backing up native
  state. Verify mode transitions and prevent native/server scheduling conflicts.
  Preserve unsupported native entries and provide an explicit rollback path.
- Evaluate a controller-native off fallback only if it can be shown to work in
  the chosen mode. Otherwise document the last-state-on-server-outage behavior;
  do not present an unverified fallback as protection.
- **Done when:** a real evening follows the configured schedule with an override
  and automatic resumption; restart and reconnect tests select the correct current
  scene. Both users can understand next week's behavior without interpreting logs.
  This is the first scheduling-focused household beta.

## Phase 5 — House photos, roofline mapping and previews (L)

Implements [photo/layout design](../design/application.md#pattern-library-and-photo-mapping)
and [photo acceptance](../specs/v1-acceptance.md#rules).

- Validate actual output counts, pixel order, direction, gaps and mirrored runs.
  Separate logical hardware addresses from photo coordinates. Any identify/calibrate
  action is short, low-brightness, user-triggered and restores the prior scene.
- Implement local photo upload with iPhone orientation and HEIC handling, bounded
  image sizes, thumbnail generation and location-metadata removal. Keep all media
  on the homelab.
- Build a touch-friendly roofline editor: polylines, run count/address range,
  reverse direction, spacing, gaps, drag handles, zoom/pan, undo/redo and numbered
  bulbs. Save normalized coordinates. Support front/side views of the same layout.
- Render patterns and animation over photos with Canvas 2D. Reuse the canonical
  pattern schema and golden logical-pixel vectors across renderer and compiler.
  Label exact/validated versus representative firmware-effect previews.
- Integrate preview into both pattern editing and calendar scene selection. Offer
  brightness and speed controls without transmitting every slider movement.
- **Done when:** a normal iPhone house photo can be uploaded and mapped, saved,
  reopened on the other phone, and used to preview a scene. A short real-light
  comparison confirms mapped order and colors for supported patterns. Approximate
  effects are labeled and uncalibrated runs cannot masquerade as verified mappings.

## Phase 6 — Operations, household trial and v1 release (M)

Implements [operations](../design/application.md#operational-notes) and the complete
[v1 acceptance contract](../specs/v1-acceptance.md).

- Produce versioned amd64/arm64 OCI images, deployment examples for the selected
  Docker/Podman or Incus environment, Caddy configuration, Pocket ID registration
  notes, persistent-volume layout and secrets configuration.
- Implement consistent backups of database/media/mappings and a manifest. Restore
  into an isolated instance with controller access disabled, then verify state and
  photos. Add migration backups, upgrade/rollback instructions and health diagnostics.
- Exercise slow/offline networks, expired login, two-user edits, browser refresh,
  service restarts, controller disconnection, missed events, duplicate service
  prevention, DST simulations and photo upload failures. Separate ordinary CI from
  explicit hardware tests. Do not silently cache authenticated writes offline.
- Complete a seven-night trial with both users; include normal nights, an exception,
  a temporary override, a controlled restart and reconnect. Record failures and
  repeat affected coverage after fixes. Schedule actual dates once the beta is ready.
- Complete the README, quick-start and recovery runbook so neither daily use nor
  recovery depends on this conversation. Tag the release after acceptance passes.
- **Done when:** both users use the hosted app for the agreed v1 tasks, the trial
  passes, backup restoration is demonstrated, and an upgrade can be rolled back.

## Later / ideas

These are outside the confirmed v1 scope: music sync, multiple-controller
orchestration, Home Assistant integration, AI-assisted roofline tracing, 3D views,
public pattern sharing, arbitrary frame streaming and controller-native calendar
export. Do not let these delay the scheduling beta or required photo feature.

## Open questions

- **Deployment details:** a TrueNAS custom app, Caddy and Pocket ID are selected.
  Private local configuration contains actual host/domain/client values. The owner
  selected public repository bketelsen/trimlights; GHCR is the image registry.
  App public exposure and away-from-home access policy remain to be confirmed.
- **Scheduling policy:** finalize priority ties, DST gaps/repeated hours, external
  app activity and outage expectations by Phase 4. Record substantive decisions
  in ADRs before implementation.
- **Physical layout:** confirm actual bulbs, outputs, mirrored runs and direction
  in Phase 2; finalize each run's mapping during Phase 5. Photos cannot infer
  wiring or create independent control where the controller mirrors outputs.
- **Preview fidelity:** classify supported static/custom effects and approximate
  firmware effects before Phase 5 acceptance; do not promise all proprietary
  animations reproduce exactly.
- **Repository identity:** public bketelsen/trimlights, selected by the owner.
  Preserve synthetic fixtures and keep deployment details private.

## References

- Implements: [application design](../design/application.md),
  [v1 acceptance](../specs/v1-acceptance.md).
- Rationale: [ADR-0003](../adr/0003-shared-homelab-application.md).
- Baseline: [current implementation](../../README.md),
  [protocol observations](../../protocol-observations.json).
- Required foundations: [clix](https://github.com/frostyard/clix),
  [agentic-template](https://github.com/bketelsen/agentic-template).

## Implementation progress — 2026-10-28

Phase 1 foundation is implemented: canonical agent instructions/symlinks, adopted
ADRs, Git initialization without a remote, clix v0.5.0, build metadata and the
hardware-free check gate. Research Python remains in place as historical evidence;
production builds/tests do not use it. Repeatable project skills remain deferred
until the corresponding workflows stabilize.

Phase 2 has a persisted local service and serialized refresh worker. See the
[implemented local contract](../specs/local-service.md). The service intentionally
has no hardware-writing endpoints yet. Dynamic layout decoding, Sunday clock
semantics, ordinary controls and Go hardware write verification remain open.
The localhost bearer API is separate from Phase 3 Pocket ID authentication.

Phase 3 now has OIDC login with synthetic-provider verification and an embedded
mobile UI for cached state, pattern names, native schedule inspection and audit
history. Real Pocket ID sign-in awaits owner-supplied client configuration. Editing,
application scene storage, household CLI login and production control actions are
not implemented. Authentication details: [contract](../specs/household-auth.md).
Deployment selection is recorded in [ADR-0004](../adr/0004-pocket-id-and-truenas.md):
TrueNAS 192.0.2.10, trimlights.example.com, issuer id.example.com. Registry,
dataset paths and public-access policy remain open; example configuration is in
`deploy/`. No homelab deployment or physical lighting changes were made in this pass.

Validation for this checkpoint: `make check` passes with the bundled Node/pnpm
runtime, including Go race tests, OIDC adversarial cases, migration backup, queue
cancellation, captured protocol vectors, TypeScript build and documentation links.
The mobile sign-in screen was inspected in the local browser. A local non-root,
read-only container successfully discovered Pocket ID over verified HTTPS, returned
readiness and rejected an unauthenticated API request. It used dummy client values
only for startup/discovery; no login or controller connection was attempted. The
test container was stopped and removed. Actual signed-in UI testing remains open.

### Shared pattern editor checkpoint — 2026-10-28

Implements [pattern design](../design/application.md#pattern-library-and-photo-mapping)
and the [pattern contract](../specs/patterns.md), following
[ADR-0005](../adr/0005-versioned-household-patterns.md).

- Application IDs and immutable revisions are persisted in SQLite schema 3.
  Version conflicts return 409; archived patterns retain history and can be restored.
- The mobile editor supports color runs, brightness, static/chase software previews,
  reverse, favorites, duplicate, JSON import/export and revision history.
- Cached controller imports preserve raw parameters and skip existing imported
  slots. Native effects remain opaque until decoding and playback are verified.
- Go and TypeScript consume shared expected logical-pixel vectors. Database tests
  cover simultaneous edits, revision retention, failed-import rollback, and schema
  upgrades preserving sessions and creating a backup.
- A test-only browser server with temporary storage and no controller access was
  used to create/edit a chase pattern, save two revisions in different tabs, observe
  a stale-editor conflict without losing the draft, reload the newer version, and
  import all 18 captured native patterns. The archive confirmation was inspected;
  archive/restore are exercised by API/storage tests. No horizontal overflow was
  observed at the browser-reported 360 CSS px width. Real iPhone Safari and household
  login remain acceptance work. The fixture server was stopped after verification.

Still required for the complete pattern feature: verified controller compilation
and playback, scheduled-reference impact UI when schedules are implemented, actual
household sign-in/testing, and photo previews. The broader v1 remains incomplete.

### Calendar planning checkpoint — 2026-10-28

Implements the [calendar contract](../specs/schedules.md), following
[ADR-0006](../adr/0006-explicit-calendar-intent.md) and the
[scheduling design](../design/application.md#scheduling-and-control-ownership).

- SQLite schema 4 retains calendar revisions, actor/time and foreign-key references
  to immutable pattern versions. Upgrade tests preserve patterns and sessions and
  confirm a pre-migration backup. Concurrent edits cannot silently overwrite.
- The Go evaluator handles weekly, seasonal, one-day, solar and overnight rules,
  override expiry, priority conflicts, default off and current-intent recovery.
  Tests include 23/25-hour days, repeated/gap clock boundaries, Lord Howe's 30-minute
  shift, Apia's skipped date, polar missing events and International Date Line dates.
- The mobile Calendar edits rules/settings/overrides, shows retained history,
  preserves conflicted drafts, and previews a timeline with precedence explanations.
  Pattern edits and archives display pinned calendar references.
- Browser fixture checks created a weekly pattern rule and one-day off exception,
  verified its precedence explanation, and observed a stale save from a second
  session preserve the first draft. The tested 360 CSS px viewport had no horizontal
  overflow. Actual iPhone Safari remains acceptance work.
- Owner-supplied OIDC config now passes CLI dry-run validation with two subjects;
  config and secret permissions are owner-only. Real sign-in remains unverified.

Still required: physical control/mode verification, native timer handoff, execution
and reconciliation through the controller worker, calendar command delivery audit,
real phone acceptance, deployment, photo mapping, backups and household trial.
Calendar planning is implemented; scheduling on the lights is not yet enabled.

### Public-source preparation — 2026-10-28

The owner selected public `bketelsen/trimlights` and requested scrubbing personal
and test data, with a configurable controller address. [ADR-0007](../adr/0007-public-repository-and-private-deployment.md)
records the policy. Original research/fixtures are retained privately; public
fixtures are synthesized byte-for-byte with neutral names and parameters.
Documentation uses example values. Compose takes its host, dataset paths, image
and timezone from variables. Probe authorization now pins the entire reviewed
non-clock baseline with SHA-256 instead of household-specific scene names.
The publication workflow builds amd64/arm64 images after the check gate.
