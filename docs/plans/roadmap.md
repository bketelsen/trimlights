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
[control rules](../specs/v1-acceptance.md#rules). The bounded playback integration
test follows [ADR-0010](../adr/0010-bounded-playback-verification.md) and the
[verification contract](../specs/playback-verification.md). Ordinary control integration
follows [ADR-0011](../adr/0011-audited-household-native-controls.md) and the
[native control contract](../specs/native-controls.md).

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
and [schedule acceptance](../specs/v1-acceptance.md#rules). Cached migration and
preflight follow [ADR-0012](../adr/0012-prepare-native-calendar-handoff.md) and the
[preparation contract](../specs/calendar-preparation.md). Delivery machinery follows
[ADR-0013](../adr/0013-journal-calendar-execution.md) and the
[dormant runtime contract](../specs/calendar-runtime.md). Internal ownership transfer
follows [ADR-0014](../adr/0014-review-calendar-ownership-transfers.md) and the
[handoff contract](../specs/calendar-handoff.md).

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
and [photo acceptance](../specs/v1-acceptance.md#rules), following
[ADR-0008](../adr/0008-private-photos-and-physical-mapping.md) and the
[house contract](../specs/house.md).

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

Backup work follows [ADR-0009](../adr/0009-consistent-portable-backups.md) and
the [backup contract](../specs/backups.md).

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
  The owner selected public HTTPS access behind Pocket ID; household subjects remain
  explicitly allowlisted.
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

### Initial homelab deployment — 2026-10-28

The public repository and amd64/arm64 image pipeline are live. The initial pinned
image passed CI and anonymous registry-pull verification. A single TrueNAS custom
app is running with private configuration and a separate writable data directory.
Caddy serves its public HTTPS origin with a valid certificate. Readiness succeeds;
unauthenticated household API requests return 401; login redirects to Pocket ID
with the exact callback, S256 PKCE, state, nonce and a secure browser-binding cookie.
The prior Caddy configuration was backed up before validation and reload.

Controller access remains disabled during real-account acceptance. Household
passkey sign-in and both phones still require user testing. The deployed build
supports shared pattern editing and calendar planning, not physical control or
schedule execution. Hostnames, addresses, credentials and deployment evidence
remain in private configuration. Deployment is an early checkpoint, not v1 release.


### House workspace checkpoint — 2026-10-28

Following [ADR-0008](../adr/0008-private-photos-and-physical-mapping.md), the
[house contract](../specs/house.md) now has an implementation:

- Bounded JPEG/PNG/WebP ingestion, eight EXIF orientations, JPEG normalization,
  metadata removal, thumbnails and authenticated media delivery. HEIC receives
  format-specific conversion advice; the picker requests supported formats.
- SQLite schema 5 stores photo blobs and immutable layout revisions with foreign
  keys. Upgrades back up schema 4 and retain sessions, patterns and calendars.
- Physical address ranges and mirrored outputs are separate from normalized photo
  views. Aspect-correct interpolation, direction and individual bulb adjustments
  are covered by shared Go/TypeScript geometry vectors.
- The House editor supports touch drawing, corner/bulb dragging, numeric corner
  coordinates, zoom/pan, rotation, undo/redo, conflict preservation and history.
- Library preview uses the current draft; Calendar preview resolves its exact
  pinned pattern revision. Neither contacts the controller. All maps are uncalibrated.
- Browser fixture verification covered upload, drawing/saving/reopening, pattern
  overlay, rotation/undo, zoom and a two-tab stale save retaining the first draft.
  A 375-pixel viewport had no horizontal overflow. Fixtures use synthetic images.

The owner confirmed real Pocket ID desktop sign-in. Actual iPhone uploads, phone
passkeys, the second household identity, physical calibration and real-light
comparison remain open. Photo deletion and layout-file import are not implemented.
This checkpoint does not enable controller writes or server-owned scheduling.


### Phone authentication acceptance — 2026-10-28

The owner confirmed phone access with a passkey after the desktop sign-in check.
This verifies real mobile authentication for the owner. It does not yet establish
second-user sign-in, home-screen behavior, ordinary iPhone photo upload, roofline
editing or cross-phone synchronization; those remain in the
[v1 acceptance contract](../specs/v1-acceptance.md).

### Portable recovery checkpoint — 2026-10-28

The Go CLI implements `backup` and `restore` under
[ADR-0009](../adr/0009-consistent-portable-backups.md) and the
[backup contract](../specs/backups.md). The [operations runbook](../design/backup-operations.md)
describes live capture and isolated recovery. Backups snapshot SQLite consistently
and include private OIDC configuration. Restore verifies archive contents and the
database, clears copied sessions/flows, and publishes a new guarded data directory.

Tests recover synthetic photos, thumbnails, all pattern/calendar/layout revisions,
observations and audits while the source store remains open. Corrupt archives,
unsafe paths, duplicate entries, symlinks, missing files, invalid schemas and
checksum mismatches fail without publishing a destination. Dry-run performs no IO;
restored controller access remains disabled despite a configured host.

A homelab recovery drill and its runtime checks are required before counting the
operational acceptance item complete. In-app backup controls, automated retention
and off-host replication remain outside this implemented CLI checkpoint.


### Recovery drill and layout draft import — 2026-10-28

The homelab recovery drill passed: a live private backup restored into a separate
network-disabled container, validated its OIDC settings, and rejected controller
refresh despite a configured host. The temporary container was removed. The actual
household backup had no photos or patterns yet; populated recovery remains covered
by synthetic tests and must be repeated after household data is added.

House now imports exported layout JSON as a validated draft, preserves current
edits on failure, and requires review/save before changing shared state. Missing
photo references require restoring the matching full backup; layout files contain
geometry only. The owner has deferred live controller testing. Physical command
verification and calibration remain pending while software development continues.


### Calendar draft recovery — 2026-10-28

Calendar import/export is available from the main calendar toolbar. Imports are
bounded, validated against the saved revision and exact pattern versions, and
loaded as drafts for preview and explicit save. Invalid imports and canceled
replacement retain current edits. Original override expiries remain unchanged.
The [calendar contract](../specs/schedules.md) defines these rules. This feature
uses no controller access; physical verification remains deferred.


### Pattern draft protection — 2026-10-28

Pattern editing now reports unsaved state to navigation and warns on browser
reload, editor exit and revision replacement. Save responses preserve newer edits
made during the request and advance the saved baseline for a subsequent save.
The [pattern contract](../specs/patterns.md) records these data-loss protections.
Live controller tests remain deferred until the owner is ready.

### Official-app control capture — 2026-10-28

The owner resumed the live test and observed OFF extinguish the lights, MANUAL
restore a saved effect, saved-pattern selection visibly change it, and TIMER
restore the scheduled effect. Private capture decoding identified the corresponding
commands, with valid encoded-frame checksums. A fresh Go snapshot confirmed TIMER,
the scheduled effect's parameter match, and unchanged saved library and schedules.
The owner reported no mention of packet drops; a zero-drop counter was not recorded.

The [snapshot contract](../specs/local-service.md#observed-playback-state--2026-10-28)
now describes bounded mode/parameter decoding with synthetic coverage for unknown
layouts and ambiguous matches. The [control design](../design/application.md#observed-native-playback--2026-10-28)
records observations and the separate owner-authorized Go verification. Physical Go
control, native timer handoff, general layout decoding and Sunday semantics remain
open. Hosted controller access remains disabled.

**Done when:** a separately authorized Go verification demonstrates OFF, saved-slot
playback and restoration to TIMER with physical observations and invariant readback;
this capture alone does not satisfy that criterion.

### Independent Go playback verification — 2026-10-28

The owner authorized the separate bounded operation in
[ADR-0010](../adr/0010-bounded-playback-verification.md), implemented under the
[verification contract](../specs/playback-verification.md) and
[control design](../design/application.md#observed-native-playback--2026-10-28).
`make check` passes with synthetic success, cancellation, disconnect, disk-error,
external-state and gated-recovery tests.

The real Go operation passed OFF and saved-slot playback readback, restored the
original saved effect and TIMER, and independently verified every non-clock byte
against the reviewed baseline on a new connection. The owner confirmed the physical
OFF → target effect → original appearance sequence. No saved patterns, schedules
or topology were changed. The previous bounded-verification done criterion is met.

Next: integrate these verified commands into the authenticated, audited controller
worker and mobile controls, with stale-library checks and ambiguous-write
reconciliation. Hosted controller access remains disabled. Custom pattern writes,
brightness, native scheduler handoff, Sunday clock encoding and calibration still
need separate work; Phase 2 and household v1 are not complete.

### Authenticated native controls — 2026-10-28

Following [ADR-0011](../adr/0011-audited-household-native-controls.md), the
[native control contract](../specs/native-controls.md) is implemented through the
[existing worker](../design/application.md#application-boundaries). Home offers
OFF, saved-controller-pattern playback and return to native TIMER. These actions
remain separate from software Library edits and Calendar execution.

Fresh baseline checks, durable pending audit, cancellation and single-read
reconciliation protect each operation. Schema 6 backs up existing databases and
retains control audit metadata; schema 5 and 6 backup round trips are supported.
Tests cover stale requests, authentication/CSRF, actor attribution, audit failure,
interrupted intents, readback failure and no automatic write retries. The frontend
build passes. Visual browser automation was unavailable in this session; actual
phone layout and signed-in control acceptance remain pending.

**Done when:** the new build is deployed with one configured controller owner, an
explicit refresh succeeds through the authenticated app, and the owner can use the
native controls. Deployment/acceptance evidence follows below; calendar ownership,
custom effects and Sunday semantics are not included in this checkpoint.

Deployment evidence: image `sha-e6ddff9d26d26687cef8cb6a37b76a58a10322ff` passed
CI and is running on TrueNAS with the controller configured. HTTPS health/readiness
return 200; anonymous status and control requests return 401. The new control UI
asset is served. Schema 6 migration preserved existing sessions and the uploaded
photo, with a pre-migration database backup. A pre-upgrade full bundle restored
with byte-identical photo/thumbnail content; a new schema 6 bundle also restored
in isolation with its controller guard. A container on TrueNAS read the controller
before the hosted worker was enabled. Startup performed no controller operations.
The owner then confirmed signed-in refresh, saved-pattern playback with MANUAL
reported, and return to the original physical appearance with TIMER reported.
The service audit independently records both controls verified without errors.
This native-control deployment checkpoint is complete. Full mobile workflow/layout
acceptance, the second household identity and the broader v1 remain open.


### Calendar handoff preparation — 2026-10-28

Following [ADR-0012](../adr/0012-prepare-native-calendar-handoff.md), Calendar now
implements the [preparation contract](../specs/calendar-preparation.md) within the
[scheduling design](../design/application.md#scheduling-and-control-ownership).
The UI checks exact pinned revisions against cached original slot bytes and shows
current/next intent, conflicts and execution blockers. It proposes daily and dated
one-day rules from native timers, preserves disabled/unsupported entries, and adds
only selected rules to the existing draft. No native schedule is changed. Saving
and controller-pattern import remain separate explicit actions.

**Done when:** synthetic checks prove revision/slot matching, preservation of
existing drafts and disabled entries, visible unsupported cases, and no controller
or storage mutations from preparation; the checked build is deployed. Sunday
clock encoding and timer-boundary behavior still require protocol evidence, then
runtime delivery/reconciliation and a real scheduled evening. Preparation does
not satisfy Phase 4 execution acceptance.


Validation/deployment: `make check` passes with 17 frontend tests, Go race tests
and 261 checked documentation links. Synthetic tests cover historical pinned
revisions, changed slots, disabled/expired references, conflicts, missing/errored
snapshots, unsupported native entries, API validation and no preparation writes.
Browser automation could not open a tab because its access host was unavailable;
visual/mobile acceptance of this panel remains outstanding.

Image `sha-d7c362582b5e8398b7171f6854e5ef3b4fbae2b8` passed CI and is running on
TrueNAS. A fresh private backup preceded the image-only update. HTTPS health and
readiness return 200, both preparation endpoints reject anonymous requests with
401, and the new UI asset is served. Schema 6, sessions, pattern/calendar revision
counts and operation history were unchanged; startup created no controller
operation. This preparation checkpoint is complete; automatic execution remains
unimplemented and disabled.


### Dormant calendar execution worker — 2026-10-28

[ADR-0013](../adr/0013-journal-calendar-execution.md) and the
[runtime contract](../specs/calendar-runtime.md) implement the next part of the
[scheduling design](../design/application.md#scheduling-and-control-ownership).
The engine runs through the existing serialized worker, resolves pinned native
slots, checks current intent after fresh reads and rejects expired or superseded
queued actions. Schema 7 journals ownership and pending delivery with its audit;
completion commits the verified checkpoint. Reconnect skips missed events,
manual controls yield ownership, and uncertain/interrupted operations pause.

**Done when:** synthetic tests prove current-intent recovery, expiring overrides,
DST intervals, external-change handling, stale queued requests, durable audit
boundaries and no ambiguous retries; migrations/backups preserve household data
and restore cannot activate execution. Production activation is explicitly outside
this checkpoint: no enable flag/endpoint or runner is started by the service.
Sunday encoding, native timer-boundary tests, the activation/handoff workflow and
a real scheduled evening remain required for Phase 4 acceptance.


Validation/deployment: `make check` passes, including Go race tests, 17 frontend
tests and 290 documentation links. Runtime cases cover clean restart/reconnect,
missed transitions, override expiry, repeated DST boundaries, external/manual
ownership changes, stale queued revisions, interval deadlines, pending/completion
storage failures and cancellation. A production-startup test verifies that even
persisted active ownership cannot query hardware. CI exposed a SQLite startup
lock race; setting its timeout before journal initialization resolved it, and the
startup test passed 100 consecutive race-enabled runs before the full gate and CI.

Image `sha-4393e2449af66140d24fee9b2b0bb394cb726161` passed CI and is running on
TrueNAS. A fresh full backup preceded the image-only configuration change. Schema
7 migration made a schema 6 backup and preserved sessions, patterns, photos,
calendar/layout revision counts and controller operation history. The persisted
runtime is disabled; startup produced no controller operation. HTTPS health and
readiness return 200; anonymous status, controls and preparation return 401.

A new schema 7 bundle restored in an isolated network-disabled container with
paused ownership, no pending delivery, cleared sessions, byte-identical photo and
thumbnail blobs, and the controller-disabled guard. Production remained disabled
for Calendar. This dormant-engine checkpoint is complete. No new physical command,
Sunday encoding, timer-boundary or production-activation acceptance is claimed.

### Reviewed ownership transfer engine — 2026-10-28

Following [ADR-0014](../adr/0014-review-calendar-ownership-transfers.md) and the
[handoff contract](../specs/calendar-handoff.md), the internal coordinator now
reviews the saved calendar's immediate action, current controller fingerprint,
ownership version and native timers. Reviews expire within one minute or at the
current intent boundary. Takeover and return-to-TIMER use the existing worker;
pending audit and paused ownership commit before delivery. Verified completion
activates Calendar or disables it for TIMER return. A concurrent pause wins.

Synthetic tests cover TIMER-to-pattern and TIMER-to-OFF takeover, continued runner
delivery after override expiry, return to native TIMER, unchanged library/timers,
stale reviews and external edits, competing calendar/ownership changes, transport
expiry, cancellation, uncertainty, pending/completion storage failures, interrupted
transfer recovery and preservation of handoff history through backup restore.
`make check` passes, including Go race tests, frontend tests, vet, documentation
links and the build. No schema migration or frontend change is needed.

There is still no HTTP/CLI activation entry point or startup runner. The review is
an internal server-created value; a future household UI must bind it to its actor.
Sunday encoding and native timer-boundary observations remain prerequisites for
that production workflow and a real scheduled-evening acceptance test. This
checkpoint changes no household schedule or controller setting.

The checked image from commit `1aba1fc` is deployed after a private schema 7 backup.
GitHub checks and both architecture builds passed. TrueNAS update completed and
the app is RUNNING; HTTPS health/readiness return 200 and unauthenticated status
returns 401. Schema, household record counts, sessions, last controller operation
and ownership version remain unchanged. Calendar remains disabled with no pending
operation. No hardware access or new physical acceptance occurred during rollout.

### Household UI cleanup — 2026-10-29

The [application UI](../design/application.md) now leads with everyday lighting
controls. Home replaces the large snapshot summary with a compact last-checked
label and refresh action. Controls have consistent panel padding, styled fields,
plain mode names and local-format timer times. Library removes revision badges
and wire-language explanations. Calendar uses routines and one planning-only
notice; imports, history, timezone setup and compatibility checks are disclosed
when needed. House moves Add roofline beside the upload action and groups light
setup and precise corner placement. Settings shows household activity without
transport diagnostics. API error responses remain available to diagnostic clients.

The [native-control contract](../specs/native-controls.md) retains clock consent,
pending audits, fresh baselines and duplicate-submit guards. The
[preparation contract](../specs/calendar-preparation.md) retains stale-review
checks and append-only copying. No execution, protocol, authentication, storage or
rendering semantics changed. Preview-only features remain labeled in plain language.

`make check` passes. Browser review used isolated synthetic lights and a generated
house illustration: native playback and timer return, library import, calendar
save/preview, house color preview, refresh-dialog cancellation and Settings activity
were exercised. Layouts were inspected at 1200 px and 390 px, with Home/Calendar
also checked at 320 px without horizontal overflow. No household controller
commands or test-data writes were made to production.
