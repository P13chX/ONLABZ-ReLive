# ReLive Control API

Initial control-plane service for ONLABZ-ReLive.

## Current scope

- persistent customer channels
- saved OBS/SRT profiles
- connection-test storage
- network recommendation engine
- PostgreSQL persistence

This service does **not** replace datarhei Core. Core remains the media engine.

## Run

From repository root:

```bash
docker compose -f docker-compose.relive.yml up --build
```

API:

```text
http://localhost:8090
```

Health:

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

The future telemetry adapter will call this automatically.

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

Example result:

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
  "reasons": ["current known-good profile is suitable"]
}
```
