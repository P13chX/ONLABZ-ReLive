# ONLABZ-ReLive — Agent Handoff / Current Development State

> **Purpose:** This file is the single handoff document for another coding agent continuing ReLive development.
>
> **Repository:** `P13chX/ONLABZ-ReLive`
>
> **Primary branch:** `2.x`
>
> **Current major implementation:** ReLive v0.8
>
> **Latest merged feature PR:** #8 — `ReLive v0.8: real input preview and super admin operations UI`
>
> **PR #8 merge commit:** `47cd4b628a0a03a20eef6c9a33a090b881d51aec`
>
> **CI policy:** GitHub Actions may be deferred because the account quota is limited. Before merge, prefer `sh scripts/premerge-check.sh` on a host that can resolve/download Go dependencies.

---

## 1. Product intent

ReLive is an ONLIVEABLE / ONLABZ live-stream relay and operations system based on the datarhei Restreamer/Core/FFmpeg ecosystem.

The product is intentionally optimized for real event operations rather than a generic streaming wizard.

Primary operating model:

```text
OBS / vMix / LiveU / mobile encoder
            │
            │ RTMP by default
            │ SRT for advanced / unstable WAN contribution
            ▼
      ReLive / datarhei Core
            │
            ├── monitor-only preview
            │
            └── independent destination workers
                    │
                    ├── YouTube
                    ├── Facebook
                    ├── TikTok
                    ├── Custom RTMP
                    └── Custom SRT
```

Core principles:

1. **Permanent channels** — customers normally reuse the same channel and configuration every job.
2. **RTMP first** — customer-facing ingest defaults to RTMP because it is familiar and simple.
3. **SRT remains available** — use for advanced contribution and unstable/mobile networks.
4. **Pass-through first** — never silently transcode production media.
5. **Independent destination workers** — a failed social destination must not restart input or other destinations.
6. **Observable** — technicians need live status, errors, network history, media integrity and incidents.
7. **Role-based UI** — customers see a simple interface; technicians see detailed controls; ONLIVEABLE super admins see the complete system.

---

## 2. Access model

ReLive currently implements three server-side roles.

### `user`

Customer/simple operator role.

Scope:

- only explicitly assigned channels through `user_channels`
- RTMP URL / Stream Key
- copy credentials
- reset/regenerate assigned channel Stream Key
- input preview
- basic destination state
- start/stop permitted destination workers

UI should remain intentionally simple.

Do not expose advanced FFmpeg/Core internals to this role by default.

### `administrator`

Experienced technician/operator role.

Scope:

- all channels under the same `owner_id`
- advanced telemetry
- SRT diagnostics/history
- destination bitstream integrity
- incidents
- destination restart controls
- create channels/users inside the same owner
- configure destinations

Must never gain global cross-owner access.

### `super_administrator`

Global ONLIVEABLE role.

Scope:

- every owner
- every user
- every channel
- every destination
- all incidents
- global operations/system health
- create all role types
- reset any Stream Key
- delete channels
- global runtime administration

The UI has dedicated Super Administrator sections:

- **Owners**
- **Users**
- **Channels**
- **System Health**

The server protects the last active super administrator from being disabled or demoted.

---

## 3. Authentication and security model

Authentication is session based.

Implemented:

- `POST /api/v1/auth/login`
- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/me`
- HttpOnly cookie
- SameSite=Lax
- optional Secure cookie using `RELIVE_COOKIE_SECURE=true`
- session token is random
- only SHA-256 token hash is stored in PostgreSQL
- passwords use salted PBKDF2-HMAC-SHA256
- no default super-admin password is committed

Bootstrap first super admin with environment variables:

```bash
RELIVE_SUPERADMIN_EMAIL=...
RELIVE_SUPERADMIN_PASSWORD=...
```

Important security rule:

> UI visibility is not the authorization boundary. Every sensitive API route must verify role/channel/owner access server-side.

### Still unresolved / must improve before public Internet deployment

High priority:

1. destination platform credentials / stream keys are still a sensitive area; move to an encrypted credential vault
2. introduce service credentials for internal workers instead of using a human super-admin-equivalent API path
3. production secret management instead of compose defaults
4. rate limiting / login brute-force protection
5. audit log expansion for user/channel/destination changes
6. CSRF posture should be reviewed if more state-changing cookie-authenticated endpoints are added
7. HTTPS reverse proxy deployment must use `RELIVE_COOKIE_SECURE=true`

Never commit:

- customer stream keys
- TikTok cookies/tokens
- YouTube/Facebook keys
- SRT passphrases
- production DB credentials
- DigitalOcean tokens
- Core admin passwords

Repository is public.

---

## 4. RTMP-first ingest

New channels default to:

```text
Protocol: RTMP
Port:     1935
App:      /live
```

Typical OBS configuration:

```text
Service:    Custom
Server:     rtmp://<public-ingest-host>:1935/live
Stream Key: rlv_<cryptographically-random-key>
```

Relevant channel fields:

- `ingest_protocol`
- `ingest_host`
- `ingest_port`
- `ingest_app`
- `stream_id`
- `stream_key`
- `stream_key_version`
- `stream_key_updated_at`

`stream_id` is the stable internal identity.

`stream_key` is rotatable.

### Stream Key reset

Endpoint:

```http
POST /api/v1/channels/{id}/stream-key/reset
```

Reset behavior:

1. generate new cryptographically-random `rlv_...` key
2. increment key version
3. update key timestamp
4. tear down active destination relay processes for the channel
5. mark running outputs reconnecting
6. tear down preview process
7. destination/preview processes reconnect using the new key
8. create persistent incident

Important limitation:

datarhei Core RTMP authentication is not a native per-channel Stream Key ACL. The old path may still be publishable to Core itself.

However, after rotation, ReLive stops subscribing/relaying the old path.

Future hardening option:

- add an ingest authorization hook/front proxy so stale keys are rejected before Core accepts a publisher

---

## 5. Production relay architecture

Destination workers are independent Core/FFmpeg processes.

Target flow:

```text
Input publisher
     │
     ├── Destination worker: YouTube
     ├── Destination worker: Facebook
     ├── Destination worker: TikTok
     ├── Destination worker: Custom RTMP
     └── Destination worker: Custom SRT
```

Default output processing:

```text
-c:v copy
-c:a copy
```

No hidden transcoding.

Destination state model currently includes:

- `idle`
- `connecting`
- `live`
- `degraded`
- `reconnecting`
- `failed`
- `disabled`
- `unknown`

Operational requirement:

> A single destination failure must not restart the contribution source or unrelated destinations.

---

## 6. Bitstream integrity / quality protection

This was added because upstream Restreamer workflows can encode before a later egress that says `copy`.

ReLive therefore explicitly compares input and output media.

Integrity states:

- `preserved`
- `changed`
- `unknown`

Current comparison:

- video codec
- resolution
- FPS
- audio stream presence
- audio codec
- audio sample rate
- audio channel count

Bitrate is displayed but not used alone as a quality-change trigger because live FFmpeg progress measurements fluctuate.

If quality changes:

- UI displays **QUALITY CHANGED**
- destination can still technically be LIVE
- incident `quality_changed` is created

Recovery creates:

- `quality_recovered`

Related document:

`docs/QUALITY_INTEGRITY.md`

---

## 7. Real Input Preview — v0.8

Overview now includes a real monitor preview.

Architecture:

```text
Input RTMP/SRT
    │
    ├── production relay workers
    │
    └── preview-only Core process
            │
            ├── -c:v copy
            ├── -c:a copy
            └── HLS -> Core memfs
                       │
                       ▼
                 ReLive nginx
                       │
                       ▼
                    Browser
```

Preview process is isolated from the production output path.

Files:

- `relive-api/preview.go`
- `relive-ui/nginx.conf`
- `relive-ui/app.js`
- `relive-ui/index.html`
- `relive-ui/styles.css`

Preview HLS naming:

```text
/preview/relive-preview-<channel>.m3u8
/preview/relive-preview-<channel>_<segment>.ts
```

Core storage:

```text
{memfs}/relive-preview-<channel>.m3u8
```

### Preview security

Never expose Core `/memfs/` as an unrestricted public path.

nginx uses `auth_request` for each preview manifest and segment request.

Backend authorization:

```text
GET /api/v1/preview-auth
```

It:

1. reads the user session
2. parses channel ID from `X-Original-URI`
3. calls server-side channel access rules
4. returns allow/deny

### Browser playback

- native HLS when supported
- hls.js fallback for MSE browsers

Current UI pins:

`hls.js 1.5.18`

Future recommendation:

- vendor hls.js locally instead of depending on public CDN in production
- add CSP/SRI or bundle asset into the UI image

---

## 8. Audio Meter

Preview starts muted.

The operator selects **Enable audio**.

The browser uses Web Audio API:

- MediaElementSource
- AnalyserNode
- time-domain RMS
- dBFS display

The meter is monitoring-only.

It does not add FFmpeg audio filters to the production relay.

Current displayed value:

- RMS dBFS

Useful future additions:

- stereo L/R meter rather than mono aggregate RMS
- peak hold
- clipping indicator
- silence timer
- audio presence alarm
- optional LUFS monitor on a dedicated analysis worker, not the production path

---

## 9. Live source media panel

Overview displays actual media observed by the preview Core/FFmpeg process input:

- video codec
- width
- height
- FPS
- video bitrate
- audio codec
- audio bitrate
- audio sample rate
- audio channel count

Do not substitute saved profile values for this panel.

Saved channel profile and observed live source are intentionally different concepts.

---

## 10. Super Administrator UI — v0.8

### Owners

Endpoints:

- `GET /api/v1/admin/owners`
- `POST /api/v1/admin/owners`
- `PATCH /api/v1/admin/owners/{id}`

Functions:

- create owner
- enable/disable owner
- show channel count
- show user count

Database table:

`owners`

Important current behavior:

legacy/current `owner_id` values from channels/users are automatically backfilled into `owners`.

### Users

Endpoints:

- `GET /api/v1/users`
- `POST /api/v1/users`
- `PATCH /api/v1/users/{id}`

Functions:

- create user
- create administrator
- create super administrator
- change role
- enable/disable
- update password through API
- assign user channels through backend payload

Current UI supports the main create/role/enable actions.

A dedicated channel-assignment editor for `user` accounts should be added.

### Channels

Endpoints include:

- `GET /api/v1/channels`
- `POST /api/v1/channels`
- `GET /api/v1/channels/{id}`
- `PATCH /api/v1/channels/{id}`
- `DELETE /api/v1/channels/{id}`
- `POST /api/v1/channels/{id}/stream-key/reset`

Current Super Admin UI supports:

- list all channels
- create channel
- select owner
- RTMP/SRT
- reset key
- delete channel

Backend supports more editable channel settings than the current UI exposes.

Add an Edit Channel drawer/modal later for:

- name
- ingest host
- protocol
- port
- ingest app
- expected resolution/FPS
- target bitrate
- audio bitrate
- SRT latency

### System Health

Endpoint:

`GET /api/v1/admin/system-health`

Current fields:

- overall status
- PostgreSQL
- datarhei Core
- owners
- users
- channels
- destinations
- live destinations
- failed destinations
- RTMP publishers
- SRT publishers
- preview processes
- critical incidents in last hour

Future useful additions:

- CPU
- RAM
- filesystem
- Docker/container state
- Core FFmpeg process counts by type
- process restart rate
- network TX/RX
- PostgreSQL size
- telemetry DB size
- host uptime
- worker/node status
- version/build SHA

---

## 11. Telemetry and connection testing

SRT telemetry uses datarhei Core experimental:

`GET /api/v3/srt`

Current useful fields include:

- RTT
- estimated bandwidth
- receive buffer
- TSBPD latency
- packet unique/loss/retransmit/drop
- bytes

Telemetry history is stored in PostgreSQL.

UI supports history ranges.

RTMP telemetry currently focuses on whether the current publisher path exists through:

`GET /api/v3/rtmp`

Do not present SRT RTT/loss metrics for an RTMP input.

### Test Connection

For RTMP:

- test detects whether publisher using the current Stream Key appears during the observation period

For SRT:

- uses RTT/loss/retransmit/bandwidth/variance logic
- recommendation engine remains rule based

Product UX principle:

> The customer should not reconfigure every event. Test the current network against the known channel/profile and recommend changes only when needed.

---

## 12. Persistent incidents

Incident table:

`incident_events`

Examples:

- destination failure
- degraded state
- audio missing
- audio recovered
- quality changed
- quality recovered
- Stream Key rotated
- RTMP test passed/failed

Technician UI includes a persistent incident timeline.

Future:

- incident acknowledgement
- resolved_at
- acknowledged_by
- notification delivery
- severity policies
- Telegram/LINE/Slack/email webhook support if desired

---

## 13. TikTok destination

Current design supports TikTok as a destination.

Two concepts:

- `manual_key`
- `external_generator`

Research previously considered:

- Loukious/TikTokStreamKeyGenerator
- Loukious/StreamLabsTikTokStreamKeyGenerator

Do **not** copy GPL code directly into the Apache-2.0 ReLive repository.

If using unofficial TikTok key generation:

- implement as a separate provider/sidecar
- keep cookies/tokens outside ReLive repository
- treat integration as experimental
- use a provider abstraction

Reference:

`docs/TIKTOK_DESTINATION.md`

---

## 14. Database model currently in use

Important tables:

- `owners`
- `users`
- `sessions`
- `user_channels`
- `channels`
- `destinations`
- `network_tests`
- `telemetry_samples`
- `incident_events`

Current migrations run inline from Go startup.

### Migration architecture debt

Inline `CREATE TABLE / ALTER TABLE` is acceptable for the prototype but should be replaced before the schema becomes much larger.

Recommended next architecture:

```text
relive-api/migrations/
  0001_initial.sql
  0002_destinations.sql
  0003_rbac.sql
  0004_preview_admin.sql
  ...
```

Use a real migration runner and track schema version.

---

## 15. Important source files

### Backend

- `relive-api/main.go` — HTTP routes, schema bootstrap, channel/test handlers
- `relive-api/auth.go` — sessions, passwords, role model, channel access
- `relive-api/ingest.go` — RTMP Stream Key generation/rotation
- `relive-api/telemetry.go` — Core RTMP/SRT telemetry
- `relive-api/telemetry_history.go` — persistence/history
- `relive-api/core_process.go` — Core process API and FFmpeg media state
- `relive-api/relay.go` — independent destination relay workers
- `relive-api/operations.go` — live runtime APIs
- `relive-api/preview.go` — preview worker and preview RBAC
- `relive-api/admin.go` — Owners/Users/Channels/System Health APIs

### Frontend

- `relive-ui/index.html`
- `relive-ui/app.js`
- `relive-ui/styles.css`
- `relive-ui/nginx.conf`

### Deployment

- `docker-compose.relive.yml`
- `relive-api/Dockerfile`
- `relive-ui/Dockerfile`

### Docs

- `docs/DEVELOPMENT_PLAN.md`
- `docs/RBAC_RTMP.md`
- `docs/QUALITY_INTEGRITY.md`
- `docs/OPERATIONS_UI.md`
- `docs/TIKTOK_DESTINATION.md`
- `docs/BRANDING.md`

---

## 16. Known technical risks / open issues

Another agent should review these before claiming production-ready status.

### P0 — build and runtime validation

The development environment used for recent changes could not resolve `github.com` from shell, so full dependency download / local Go build was not executed after v0.8.

On a real development/deployment host, run:

```bash
sh scripts/premerge-check.sh
docker compose -f docker-compose.relive.yml build
docker compose -f docker-compose.relive.yml up
```

Then validate all migrations against both:

- fresh PostgreSQL database
- existing database upgraded from v0.6/v0.7

### P0 — run real preview validation

Test:

- RTMP H.264 + AAC
- 1080p50
- 1080p59.94
- browser native HLS
- browser hls.js fallback
- audio toggle
- RBAC attempt against another user's preview URL
- Stream Key rotation while preview is active

### P0 — destination secret storage

Destination Stream Keys must move out of plaintext operational tables.

Preferred direction:

- AES-256-GCM at application layer
- master key from deployment secret
- credential IDs referenced by destinations
- never return secret values through list APIs

### P1 — remove CDN dependency for hls.js

Current HTML references a pinned public CDN build.

Vendor or bundle hls.js locally.

### P1 — preview worker resource lifecycle

Preview is created when Overview polls `/preview`.

Current lifecycle stops/recreates on:

- key rotation
- channel edit
- channel delete

Potential improvement:

- stop preview after no authorized viewer for N minutes
- avoid one long-running preview worker for every channel that has ever been opened
- expose Preview Start/Stop or heartbeat/viewer lease

### P1 — user channel assignment UI

Backend supports `ChannelIDs` for user updates.

Super Admin UI should add an assignment control so a `user` can be given one or multiple channels without using raw API.

### P1 — owner disable semantics

Today owner `active=false` is metadata/UI state.

Define whether disabling an owner should also:

- prevent login
- stop destinations
- block ingest relay
- disable all channels
- leave live streams untouched until manual stop

Do not assume semantics; implement explicitly.

### P1 — channel delete safety

Current Super Admin can delete a channel.

Consider:

- typed confirmation
- prevent delete while LIVE
- archive/soft-delete instead of destructive delete
- export audit info before deletion

### P1 — RTMP per-key ingest enforcement

Current key rotation controls what ReLive subscribes to, not what Core accepts.

For stronger isolation implement a gate in front of Core publishing or an authorization hook.

### P1 — telemetry semantics

Be careful:

- `bandwidth_mbit` from SRT is estimated link bandwidth, not media bitrate
- cumulative packet counters should be shown as deltas/rates for alerting
- UI thresholds should move to server-side configurable alert policies

### P1 — Core `/api/v3/srt` is experimental

Add capability/version detection and graceful fallback.

### P2 — preview compatibility

Stream-copy HLS assumes browser-compatible media.

Standard profile is H.264 + AAC.

For HEVC/AV1 sources consider a **preview-only** low-bitrate H.264/AAC transcode worker.

Never put that preview transcode in the production relay path.

---

## 17. Recommended next development order

### Milestone 1 — validate v0.8 on a real deployment

Do this before adding large new features.

1. build containers
2. run migrations
3. bootstrap super admin
4. create owner
5. create administrator
6. create user
7. assign channel
8. publish OBS RTMP 1080p50
9. verify preview
10. verify audio meter
11. verify media telemetry
12. test YouTube/Facebook relay
13. rotate key during operation
14. verify old key no longer relays
15. test role isolation
16. test preview URL isolation
17. leave running for several hours and inspect memory/CPU/process leaks

### Milestone 2 — encrypted credential vault

Priority security work.

### Milestone 3 — finish management UX

- user ↔ channel assignment UI
- channel edit drawer
- destination CRUD UI
- confirmation flows
- owner operational semantics
- audit log UI

### Milestone 4 — production observability

- system CPU/RAM/disk/network
- process health
- destination event history
- alert policy engine
- notification delivery
- audio silence/clipping monitoring
- longer-term telemetry aggregation

### Milestone 5 — resilience

- SRTLA / multipath contribution experiments
- multi-node workers
- worker scheduling
- standby worker
- distribution failover

---

## 18. Upstream Restreamer issues relevant to ReLive

Previous audit found issues/reports worth regression testing:

- Restreamer #787 — incorrect/duplicate FFmpeg filter construction
- Restreamer #943 — conflicting frame-rate passthrough arguments
- Restreamer #728 — YouTube audio stutter regression
- Restreamer #552 — passthrough/HDR forwarding concerns

This is one reason ReLive should keep the production relay architecture explicit and stream-copy-first instead of relying on generic Restreamer encoding presets.

Do not assume every upstream bug affects ReLive directly.

---

## 19. Quality profile philosophy

Recommended source profile for mainstream social delivery:

```text
Video: H.264
Resolution: 1920×1080
FPS: 50 or 59.94
Rate control: CBR
Keyframe/GOP: 2 sec
Audio: AAC-LC
Sample rate: 48 kHz
Channels: stereo
```

Typical bitrate depends on network and destination.

ReLive must not silently lower:

- codec
- FPS
- resolution
- bitrate target
- sample rate
- channel count

If transcoding is ever added to UI:

```text
Processing Mode
● Direct / Preserve Source
○ Audio conversion only
○ Video transcode
○ Full transcode
```

Any non-direct mode must show a clear quality-change warning.

---

## 20. UX rules to preserve

### Customer / user

Login should feel like opening a permanent channel, not creating a new stream every event.

Primary actions:

- see input status
- see preview
- RTMP URL
- Stream Key
- Copy
- Reset key
- destination status
- Start / Stop

Avoid exposing:

- FFmpeg command lines
- Core process IDs
- low-level worker IDs
- unnecessary networking knobs

### Administrator

Expose:

- input preview
- audio meter
- live codec/FPS/bitrate
- network telemetry
- history graph
- destination integrity
- incidents
- restart controls
- advanced configuration

### Super Administrator

Expose global system structure and operational control.

Never mix global cross-customer data into normal Administrator views.

---

## 21. Development / merge policy

The user has explicitly preferred active implementation over long design-only discussion.

Normal workflow:

1. inspect current `2.x`
2. create a focused feature branch
3. implement
4. self-review
5. run local checks when environment permits
6. create a ready PR
7. merge into `2.x` without waiting for GitHub Actions when CI quota is unavailable, unless the change is clearly unsafe/breaking

Local check:

```bash
sh scripts/premerge-check.sh
```

It should run:

- `gofmt -l .`
- `go test ./...`
- `go vet ./...`
- `go build ./...`

For media changes also perform an actual stream test. Compile success is not sufficient.

---

## 22. Completed feature PR sequence

Useful historical checkpoints:

- **PR #1** — v0.1 permanent channels / cellular test foundation
- **PR #2** — v0.2 SRT telemetry lifecycle / TikTok destination
- **PR #3** — v0.3 Technician Console / destination runtime monitoring
- later relay/history/security work led into v0.6
- **PR #6** — v0.6 quality integrity guardrails / audit fixes
- **PR #7** — v0.7 three-tier RBAC / RTMP-first ingest
- **PR #8** — v0.8 real input preview / audio meter / Super Admin operations UI

When debugging regressions, compare behavior against these milestones.

---

## 23. Source of truth warning

Some older roadmap text in README or older docs may still describe RBAC/customer UI as future work even though v0.7/v0.8 implemented it.

For current implementation state, prefer:

1. current `2.x` source
2. this `docs/AGENT_HANDOFF.md`
3. `docs/OPERATIONS_UI.md`
4. `docs/RBAC_RTMP.md`
5. `docs/QUALITY_INTEGRITY.md`

If documentation conflicts with source, inspect current source before changing behavior.

---

## 24. Definition of “production ready” for the next agent

Do not mark ReLive production-ready until at minimum:

- current `2.x` builds cleanly
- migrations pass on clean + upgraded DB
- RTMP publish tested from OBS
- preview tested
- audio meter tested
- user/admin/super-admin isolation tested
- destination relay tested for at least YouTube + Facebook + custom RTMP
- Stream Key rotation tested while live
- long-duration stream tested
- credential storage hardened
- HTTPS/session cookie deployment tested
- backup/restore of PostgreSQL tested
- basic operational runbook written

The next agent should treat **real deployment validation and secret hardening** as higher priority than adding more UI decoration.
