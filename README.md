# ONLABZ-ReLive

**ONLABZ-ReLive** is a production-oriented fork of [datarhei Restreamer 2.x](https://github.com/datarhei/restreamer) focused on reliable remote contribution and multi-destination distribution.

The project is being adapted for a common ONLIVEABLE workflow:

```text
Customer / Remote Site
OBS Studio
    │
    │ SRT preferred
    │ RTMP/RTMPS fallback
    ▼
ONLABZ-ReLive
    │
    ├── pass-through → YouTube
    ├── pass-through → Facebook
    ├── pass-through → Custom RTMP/SRT
    └── pass-through → Backup
```

The priority is **stable contribution over cellular / mixed internet**, simple customer operation, and **pass-through-first** distribution that preserves the source video and audio whenever the destination is compatible.

---

## Project status

Active development is merged progressively into branch `2.x`; feature branches are used for each implementation batch and merged after review.

### Implemented

- Restreamer 2.x / datarhei Core retained as the media-engine foundation
- ReLive Control API in Go
- PostgreSQL control-plane storage
- Permanent customer channels and saved OBS/SRT profiles
- Automatic SRT connection test and recommendation engine
- datarhei Core SRT telemetry adapter
- Persistent SRT telemetry history
- Debounced cellular/network incident detection
- Technician Console on port 8088
- Channel-specific destinations
- Independent datarhei Core / FFmpeg process per destination
- Pass-through-first relay (`-c:v copy -c:a copy`)
- Destination Start / Stop / Restart controls
- Per-destination video bitrate, audio bitrate, FPS and audio PPS
- Audio health states and missing-audio incidents
- Destination reconnect/error monitoring
- Persistent incident timeline
- TikTok destination model with manual-key support
- Docker Compose development stack

### Still in development

- Customer authentication
- Admin/User RBAC and authenticated ownership enforcement
- Customer-facing simplified channel UI
- Encrypted destination credential vault
- TikTok external-generator sidecar
- SRTLA / multipath contribution
- WHIP contribution
- Multi-node worker scheduler and failover
- Long-term analytics beyond the current configurable telemetry retention

---

# Product philosophy

## Permanent channels, not per-event setup

Customers should not need to recreate a stream configuration for every job.

A customer signs in and sees the same assigned channel:

```text
My Channel
────────────────────────────

Chiang Mai Main
Status: OFFLINE

Known-good profile
1080p50
Video 5.5 Mbps
Audio 192 kbps
SRT latency 750 ms

[Test Connection]
[View Settings]
[Go Live]
```

OBS is normally configured once during onboarding.

Before an event, the customer runs **Test Connection**. ReLive evaluates the current connection and either keeps the existing known-good profile or recommends only the settings that should change.

Example:

```text
Current
Video bitrate  5.5 Mbps
SRT latency    750 ms

Recommended today
Video bitrate  4.5 Mbps
SRT latency    1000 ms

Resolution     Keep 1080p
FPS            Keep 50
Audio          Keep 192 kbps
```

---

# Pass-through first

The default media policy is:

```text
VIDEO = COPY
AUDIO = COPY
```

Equivalent FFmpeg concept:

```bash
-c:v copy
-c:a copy
```

ReLive should avoid unnecessary:

- decoding
- re-encoding
- scaling
- frame-rate conversion
- audio resampling
- loudness normalization

Transcoding is a fallback for destination compatibility, not the normal media path.

This is especially important for live music and production audio where another AAC encode generation is undesirable.

---

# Contribution strategy

## Recommended: OBS → SRT → ReLive

SRT is the preferred contribution transport for remote sites and cellular internet.

```text
OBS
 │
 │ SRT
 ▼
ReLive
 │
 ├── RTMP/RTMPS → YouTube
 ├── RTMP/RTMPS → Facebook
 └── SRT/RTMP   → downstream
```

RTMP/RTMPS remains available for compatibility.

### Initial known-good profile

A practical starting point for a regular customer channel:

```text
Resolution     1920x1080
FPS            50
Video          H.264
Video bitrate  5500 kbps
Audio          AAC-LC
Audio bitrate  192 kbps
Sample rate    48 kHz
SRT latency    750 ms
```

The connection-test engine should prefer keeping this profile if current network quality is acceptable.

---

# Connection recommendation engine

The first implementation is rule-based and intentionally conservative.

Current API result states:

- `KEEP`
- `INCREASE_LATENCY`
- `REDUCE_BITRATE`
- `SAFE_PROFILE`
- `REVIEW`

Current telemetry input:

- average RTT
- maximum RTT
- packet loss
- retransmit rate
- bitrate variance
- audio drop count
- reconnect count

General policy:

```text
Good network
→ keep known-good settings

Moderate loss / high RTT
→ keep quality, increase SRT latency first

Unstable network
→ increase latency + reduce video bitrate

Severe instability
→ recommend safe profile

Audio drop / reconnect
→ flag operator review
```

The server should not silently change OBS settings.

---

# Architecture

```text
                        ReLive UI
                           │
                           ▼
                  ReLive Control API
                           │
              ┌────────────┴────────────┐
              ▼                         ▼
          PostgreSQL              datarhei Core
                                        │
                                        ▼
                                      FFmpeg
                                        │
                   ┌────────────────────┼──────────────────┐
                   ▼                    ▼                  ▼
                YouTube             Facebook          Custom
```

The control plane is intentionally separated from datarhei Core.

Core remains focused on media/process handling while ReLive adds:

- users
- ownership
- permanent channels
- recommendations
- destination management
- telemetry history
- audit controls

This reduces the amount of upstream Core code that needs to be forked.

---

# Repository layout

```text
ONLABZ-ReLive/
├── Dockerfile
├── run.sh
├── docker-compose.relive.yml
│
├── relive-api/
│   ├── Dockerfile
│   ├── go.mod
│   ├── main.go
│   └── README.md
│
├── docs/
│   └── DEVELOPMENT_PLAN.md
│
└── ui-root/
```

The repository still contains the original Restreamer bundle structure.

---

# ReLive Control API

The initial Control API is written in Go and uses PostgreSQL.

Default local endpoint:

```text
http://localhost:8090
```

## Run the development stack

```bash
docker compose -f docker-compose.relive.yml up --build
```

Services:

| Service | Purpose | Default port |
|---|---|---:|
| Restreamer/Core | Media engine | 8080 |
| Restreamer HTTP | Media HTTP | 8181 |
| RTMP | Media ingest | 1935 |
| RTMPS/secondary RTMP | Media ingest | 1936 |
| SRT | Contribution | 6000/udp |
| ReLive API | Control plane | 8090 |
| PostgreSQL | Internal database | internal only |

## API health

```bash
curl http://localhost:8090/health
```

## Create a permanent channel

```bash
curl -X POST http://localhost:8090/api/v1/channels \
  -H 'content-type: application/json' \
  -d '{
    "owner_id":"customer-001",
    "name":"Chiang Mai Main",
    "ingest_protocol":"srt",
    "ingest_host":"ingest.example.com",
    "ingest_port":10001,
    "stream_id":"customer-001-main",
    "resolution":"1920x1080",
    "fps":50,
    "video_bitrate_kbps":5500,
    "audio_bitrate_kbps":192,
    "srt_latency_ms":750
  }'
```

## Automatic connection test

Start a real SRT/Core-backed test:

```bash
curl -X POST http://localhost:8090/api/v1/channels/1/test/start
```

ReLive samples the active SRT publisher, evaluates RTT/loss/retransmit/bitrate stability, stores the result, and finishes the channel as `READY` or `DEGRADED`.

Persistent telemetry:

```http
GET /api/v1/channels/1/telemetry/live
GET /api/v1/channels/1/telemetry/history?minutes=15
```

More API examples are in [relive-api/README.md](relive-api/README.md).

---

# Cellular resilience roadmap

ReLive is intended to work well for remote production locations where connectivity may be a mixture of:

- venue fiber
- AIS / TRUE 4G/5G
- portable 5G routers
- shared venue Wi-Fi

The platform cannot create bandwidth that does not exist. Therefore the design separates two problems:

### Recoverable instability

SRT can help with:

- packet loss
- jitter
- short interruptions
- retransmission
- variable RTT

### Insufficient sustained bandwidth

If the uplink cannot continuously carry the OBS bitrate, the correct action is to reduce contribution bitrate at OBS.

ReLive should recommend that change rather than receiving an oversized stream and transcoding it after the bottleneck.

Future advanced contribution modes will investigate:

- SRTLA / multipath
- multi-network bonding
- WHIP
- alternate gateway implementations

---

# Destination isolation

The target distribution design uses independent destination relay processes.

```text
Original stream
      │
      ├── relay → YouTube
      ├── relay → Facebook
      ├── relay → TTM
      └── relay → Backup
```

A Facebook reconnect must not restart:

- the ingest stream
- YouTube
- another destination

This isolation is implemented with one datarhei Core / FFmpeg process per destination. Each worker subscribes to the already-published internal SRT resource, so multiple outputs do not open multiple contribution connections back to the customer's OBS encoder.

See [docs/RELAY_WORKER.md](docs/RELAY_WORKER.md).

---


# TikTok LIVE destination

ReLive now includes TikTok in the destination platform catalog.

Current key modes:

- `manual_key` — use a TikTok RTMP URL/key supplied by the account owner
- `external_generator` — reserved for an isolated key-generation sidecar

The external-generator path is intentionally separated from the Apache-2.0 ReLive binary because current community TikTok LIVE generators are unofficial and may use different licenses or external account/session mechanisms.

Candidate projects evaluated:

- `Loukious/StreamLabsTikTokStreamKeyGenerator` — simpler Streamlabs-based key generation flow
- `Loukious/TikTokStreamKeyGenerator` — newer LIVE room + FFmpeg proxy + LIVE Studio-style metadata flow

See [docs/TIKTOK_DESTINATION.md](docs/TIKTOK_DESTINATION.md) for the integration contract and security boundaries.



# Technician Console

ReLive includes a technician-facing operations UI at:

```text
http://localhost:8088
```

The console polls operational state every 2 seconds and shows:

- channel selector and Test Connection
- SRT input LIVE/OFFLINE state
- RTT, estimated link capacity, SRT latency and receive buffer
- persistent RTT and receive-bitrate history (5 min / 15 min / 1 hour)
- known-good contribution profile and recommendation
- destination LIVE / DEGRADED / RECONNECTING / FAILED state
- per-destination video bitrate
- per-destination audio bitrate and audio PPS
- FPS
- reconnect count and redacted last error
- Start / Stop / Restart controls
- SRT packet counters
- persistent incident timeline
- attention banner for failed/degraded/audio-missing outputs

Destination relay runtime comes from datarhei Core FFmpeg process state, rather than UI-only simulated status.

Telemetry is persisted in PostgreSQL. Default retention is 24 hours and can be changed with `RELIVE_TELEMETRY_RETENTION`.

# Development roadmap

See [docs/DEVELOPMENT_PLAN.md](docs/DEVELOPMENT_PLAN.md).

Immediate priorities:

1. Add encrypted destination credential storage.
2. Add Admin/User authentication and server-side RBAC.
3. Build the simplified customer-facing permanent-channel UI.
4. Implement the isolated TikTok external-generator sidecar.
5. Add notification delivery for critical incidents.
6. Validate long-duration pass-through relay under real cellular contribution.
7. Evaluate SRTLA / multipath bonding.
8. Add multi-node worker scheduling and failover.

---

# Upstream Restreamer

This project is based on the Restreamer 2.x bundle and continues to use the Restreamer/Core/FFmpeg ecosystem.

Upstream projects:

- [datarhei/restreamer](https://github.com/datarhei/restreamer)
- [datarhei/core](https://github.com/datarhei/core)
- [datarhei/restreamer-ui](https://github.com/datarhei/restreamer-ui)
- [datarhei/ffmpeg](https://github.com/datarhei/ffmpeg)

The `2.x` branch is intentionally kept close to upstream while ReLive development takes place on dedicated branches.

---

# License

The upstream Restreamer license remains applicable to the inherited project code. See [LICENSE](LICENSE).
