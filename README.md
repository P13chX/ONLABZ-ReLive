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

Current development branch:

```text
relive-v0.1-mvp
```

The original Restreamer 2.x bundle remains available on branch `2.x` as the upstream baseline.

### Implemented in the ReLive development branch

- Restreamer 2.x bundle retained as the media-engine foundation
- ReLive Control API sidecar
- PostgreSQL control-plane storage
- Persistent customer channels
- Saved OBS/SRT channel profiles
- Connection-test data model
- Rule-based network recommendation engine
- Known-good-profile-first workflow
- ReLive health endpoint
- Docker Compose development stack
- Initial development roadmap

### In development / not yet complete

- Automatic SRT telemetry collection from the media engine
- Customer login / authentication
- Admin/User RBAC
- Channel ownership enforcement from authenticated identity
- Customer web UI
- Test Connection workflow in the UI
- Restreamer/Core telemetry adapter
- Independent per-destination relay worker orchestration
- Audio packet/drop watchdog
- Destination credential vault
- Cellular historical analytics
- SRTLA / multipath contribution
- WHIP contribution
- Multi-node scheduling

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

## Submit a connection test

At this stage telemetry is submitted to the API explicitly. A Core/SRT telemetry adapter is the next implementation milestone.

```bash
curl -X POST http://localhost:8090/api/v1/channels/1/tests \
  -H 'content-type: application/json' \
  -d '{
    "rtt_avg_ms":95,
    "rtt_max_ms":180,
    "packet_loss_pct":0.8,
    "retransmit_pct":1.1,
    "bitrate_variance_pct":8,
    "audio_drop_count":0,
    "reconnect_count":0
  }'
```

Example:

```json
{
  "result": "KEEP",
  "network_health": "GOOD",
  "current_video_bitrate_kbps": 5500,
  "recommended_video_bitrate_kbps": 5500,
  "current_srt_latency_ms": 750,
  "recommended_srt_latency_ms": 750,
  "keep_resolution": true,
  "keep_fps": true,
  "keep_audio": true,
  "reasons": [
    "current known-good profile is suitable"
  ]
}
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

This orchestration layer is planned but is not yet implemented in the current branch.

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


# Development roadmap

See [docs/DEVELOPMENT_PLAN.md](docs/DEVELOPMENT_PLAN.md).

Immediate priorities:

1. Connect real SRT/Core telemetry to the connection-test API.
2. Add channel test lifecycle: `OFFLINE → TESTING → READY`.
3. Add customer authentication and Admin/User ownership.
4. Build the customer channel page around existing permanent channels.
5. Implement pass-through destination workers with isolated reconnect.
6. Add audio continuity monitoring.
7. Add telemetry history and operator dashboard.
8. Evaluate SRTLA / multipath after the standard OBS→SRT workflow is stable.

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
