# ReLive Development Plan

## Product direction

ReLive is being extended from the Restreamer 2.x bundle into an operator-friendly cloud contribution and distribution service.

Primary customer flow:

```text
OBS (customer)
  │
  │ SRT preferred / RTMPS fallback
  ▼
ReLive
  │
  ├── pass-through → YouTube
  ├── pass-through → Facebook
  └── pass-through → custom destinations
```

Customers are not expected to recreate stream settings for every event.

Each customer receives a persistent channel with a known-good OBS profile. Before an event, the customer can run a connection test. ReLive compares current cellular/network conditions against the stored profile and recommends only the settings that should change.

## Phase 1 — Permanent Channel + Test Recommendation

Implemented in the initial control API:

- persistent channel records
- per-channel OBS/SRT profile
- network test records
- rule-based recommendation engine
- known-good-profile-first behavior
- health endpoint
- PostgreSQL persistence

Initial recommendation states:

- `KEEP`
- `INCREASE_LATENCY`
- `REDUCE_BITRATE`
- `SAFE_PROFILE`
- `REVIEW`

The current implementation is an API foundation. SRT statistics still need to be fed by the media gateway/Core telemetry adapter.

## Phase 2 — Gateway telemetry adapter

Add an internal adapter to collect:

- SRT RTT
- packet loss
- retransmit rate
- bitrate variance
- reconnect count
- audio packet continuity
- audio drop events

The adapter writes a completed test to:

`POST /api/v1/channels/{id}/tests`

## Phase 3 — Customer UI

Login should land on existing channels, not a create-stream wizard.

Channel card:

- channel status
- current known-good profile
- last test time
- last live time
- network health
- Test Connection
- View Settings
- Go Live

Test result compares:

- current profile
- recommended profile

Keep resolution/FPS/audio unchanged whenever possible. Prefer adjusting SRT latency first, then video bitrate.

## Phase 4 — Pass-through relay isolation

For compatible H.264 + AAC sources:

```text
-c:v copy
-c:a copy
```

Each destination must be independently restartable.

A failed Facebook output must not restart YouTube or the ingest pipeline.

## Phase 5 — Multi-user and RBAC

Roles:

- Admin
- User

Server-side ownership must apply to:

- channels
- destinations
- credentials
- tests
- telemetry
- logs

## Phase 6 — Cellular resilience

Profiles should be conservative and based on historical performance.

Suggested starting points:

| Profile | Video | Audio | SRT latency |
|---|---:|---:|---:|
| Normal | 5.5 Mbps | 192 kbps | 750 ms |
| Safe | 4.5 Mbps | 192 kbps | 1000–1200 ms |
| Very Safe | 3.0 Mbps | 160–192 kbps | 1500 ms |

Do not transcode on the server to compensate for an uplink that cannot carry the OBS source bitrate. Reduce contribution bitrate at the sender when required.

## Phase 7 — Advanced contribution

Later:

- SRTLA / multipath contribution
- WHIP ingest
- multi-node workers
- alerting
- historical graphs
- automatic recommendation refinement
