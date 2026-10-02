# ReLive Control API

Control plane and operations API for ONLABZ-ReLive.

datarhei Core remains the media/process engine. ReLive adds permanent channels, network testing, destination ownership, relay orchestration, telemetry history, and technician operations.

## Run

From repository root:

```bash
docker compose -f docker-compose.relive.yml up --build
```

Endpoints:

```text
ReLive Technician Console  http://localhost:8088
ReLive API                 http://localhost:8090
Restreamer/Core            http://localhost:8080
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
    "ingest_port":6000,
    "stream_id":"customer-001-main",
    "resolution":"1920x1080",
    "fps":50,
    "video_bitrate_kbps":5500,
    "audio_bitrate_kbps":192,
    "srt_latency_ms":750
  }'
```

## Automatic connection test

```bash
curl -X POST http://localhost:8090/api/v1/channels/1/test/start
```

ReLive samples Core SRT telemetry and automatically finishes the channel test as READY or DEGRADED.

## Create a destination

Destinations belong to a permanent channel.

```bash
curl -X POST http://localhost:8090/api/v1/destinations \
  -H 'content-type: application/json' \
  -d '{
    "channel_id":1,
    "owner_id":"customer-001",
    "name":"YouTube Main",
    "platform":"youtube",
    "key_source":"manual_key",
    "server_url":"rtmps://a.rtmp.youtube.com/live2",
    "stream_key":"YOUR_KEY"
  }'
```

The stream key is not returned after creation.

## Start / stop / restart destination

```bash
curl -X POST http://localhost:8090/api/v1/destinations/1/command \
  -H 'content-type: application/json' \
  -d '{"command":"start"}'
```

Commands:

```text
start
stop
restart
```

## Destination runtime

```http
GET /api/v1/destinations/runtime?channel_id=1
```

Runtime data includes:

- status
- Core process ID
- output bitrate
- video bitrate
- audio bitrate
- FPS
- audio PPS
- audio health
- reconnect count
- latest redacted error

## Persistent SRT telemetry

```http
GET /api/v1/channels/1/telemetry/live
GET /api/v1/channels/1/telemetry/history?minutes=15
```

## Incidents

```http
GET /api/v1/incidents?channel_id=1
```

See [../docs/RELAY_WORKER.md](../docs/RELAY_WORKER.md) for relay architecture.
