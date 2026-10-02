# ReLive Destination Relay Worker

## Goal

Relay one customer contribution stream to multiple destinations without opening multiple upstream connections to the customer's OBS/mobile encoder.

The media path is:

```text
OBS / remote encoder
      │
      │ SRT publish
      ▼
datarhei Core SRT server
      │
      │ internal SRT subscribe (mode=request)
      ├──────────────┬──────────────┐
      ▼              ▼              ▼
Relay process 1  Relay process 2  Relay process 3
YouTube          Facebook         Custom / TikTok
```

Each destination is an independent datarhei Core FFmpeg process.

## Pass-through policy

For compatible RTMP/RTMPS destinations:

```text
-c:v copy
-c:a copy
-f flv
```

For SRT destinations:

```text
-c:v copy
-c:a copy
-f mpegts
```

No decode/re-encode is intended in the normal relay path.

## Internal SRT source

The relay worker subscribes to the already-published SRT resource inside Core.

Conceptually:

```text
srt://restreamer:6000
  ?mode=caller
  &transtype=live
  &streamid=#!::r=<channel-stream-id>,m=request
```

Optional token and encryption settings:

- `RELIVE_INTERNAL_SRT_TOKEN`
- `RELIVE_INTERNAL_SRT_PASSPHRASE`
- `RELIVE_INTERNAL_SRT_PBKEYLEN` (16/24/32)

This prevents every destination worker from reconnecting to the customer's original contribution endpoint.

## Destination lifecycle

Each destination has two different concepts:

### desired_state

Operator intent:

```text
stopped
running
```

### runtime status

Observed state:

```text
UNKNOWN
IDLE
CONNECTING
LIVE
DEGRADED
RECONNECTING
FAILED
DISABLED
```

Technician commands:

```http
POST /api/v1/destinations/{id}/command
Content-Type: application/json

{"command":"start"}
{"command":"stop"}
{"command":"restart"}
```

## Process isolation

Every destination gets a separate Core process identified by:

```text
reference = relive-destination:<destination-id>
```

A failed/reconnecting Facebook process must not restart:

- the SRT publisher
- YouTube
- TikTok
- another custom destination

## Media telemetry

The relay manager polls Core process state and reads per-stream FFmpeg progress.

Stored runtime metrics:

- total output bitrate
- video bitrate
- audio bitrate
- FPS
- audio packet rate (PPS)
- audio health
- reconnect count
- last FFmpeg error/log line

Audio states:

```text
healthy
missing
unknown
```

`missing` means an audio stream is present on the process input while no audio packets/bitrate are observed on the output.

The relay path itself remains stream-copy. Audio health is inferred from Core/FFmpeg progress rather than adding a decode stage to the media path.

## Incident persistence

Important destination transitions are persisted in `incident_events`.

Examples:

- destination becomes FAILED
- destination becomes RECONNECTING
- destination becomes DEGRADED
- audio becomes missing
- audio recovers

API:

```http
GET /api/v1/incidents?channel_id=<id>&limit=100
```

## Persistent contribution telemetry

SRT contribution telemetry is sampled every 2 seconds by default and stored in PostgreSQL.

Metrics:

- RTT
- estimated link bandwidth
- measured receive bitrate
- receive buffer
- SRT latency
- receive loss packet counter
- retransmit packet counter
- drop packet counter

History API:

```http
GET /api/v1/channels/{id}/telemetry/history?minutes=15
```

Default retention:

```text
24 hours
```

Configure with:

```text
RELIVE_TELEMETRY_INTERVAL=2s
RELIVE_TELEMETRY_RETENTION=24h
```

## Security note

Destination stream keys are currently stored by the existing v0.x schema and are never returned by normal list/runtime APIs.

Production hardening still requires replacing plaintext destination secret storage with encrypted server-side credential storage.

Core process state may internally contain the FFmpeg command line. ReLive does not expose that command in the Technician Console.

Runtime errors are redacted against the configured stream key before being persisted to ReLive incident/status fields.

## TikTok

TikTok `manual_key` destinations can use the normal relay worker.

TikTok `external_generator` remains intentionally blocked from starting until the isolated generator sidecar is implemented. This avoids treating an unofficial account/session integration as production-ready.
