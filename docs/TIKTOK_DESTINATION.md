# TikTok LIVE Destination

ReLive supports TikTok as a destination platform.

## Current modes

### 1. manual_key

Use when the TikTok account already provides an RTMP server URL and stream key.

Example:

```json
{
  "owner_id": "customer-001",
  "name": "TikTok Main",
  "platform": "tiktok",
  "key_source": "manual_key",
  "server_url": "rtmp://push.rtmp.tiktok.com/live",
  "stream_key": "..."
}
```

This is the lowest-risk path because ReLive only forwards to credentials supplied by the account owner.

### 2. external_generator

Reserved for a separate service that creates/refreshes TikTok LIVE push credentials.

Example:

```json
{
  "owner_id": "customer-001",
  "name": "TikTok Main",
  "platform": "tiktok",
  "key_source": "external_generator",
  "generator_ref": "customer-001-tiktok"
}
```

ReLive intentionally does not embed unofficial TikTok credential-generation code into the main binary.

## Candidate upstream projects

### Loukious/StreamLabsTikTokStreamKeyGenerator

Repository:

```text
https://github.com/Loukious/StreamLabsTikTokStreamKeyGenerator
```

The project generates TikTok LIVE stream URL/key using Streamlabs access.

The upstream author currently recommends this project for the simpler flow.

Important constraints:

- requires TikTok LIVE access through Streamlabs
- unofficial integration
- upstream API behavior can change
- GPL-3.0 project
- should run outside the Apache-2.0 ReLive binary

### Loukious/TikTokStreamKeyGenerator

Repository:

```text
https://github.com/Loukious/TikTokStreamKeyGenerator
```

This project can:

- create a TikTok LIVE room
- retrieve TikTok push URL/key
- use an FFmpeg proxy
- inject LIVE Studio-style metadata
- use an external signer service

This path has more moving parts and should be treated as experimental.

The repository notes that directly using the real TikTok push URL/key from OBS may not behave like TikTok LIVE Studio because the official desktop software sends additional metadata.

## Recommended ReLive architecture

```text
ReLive Control API
      │
      │ internal HTTPS
      ▼
TikTok Generator Sidecar
      │
      │ TikTok/Streamlabs session
      ▼
TikTok LIVE room
      │
      ├── push URL
      └── temporary key
      │
      ▼
ReLive Destination Worker
      │
      │ H.264/AAC pass-through where compatible
      ▼
TikTok LIVE
```

The sidecar owns all account/session-specific credentials.

ReLive stores only a `generator_ref` and receives temporary destination information when a stream starts.

## Proposed sidecar contract

### Activate destination

```http
POST /v1/live/activate
Authorization: Bearer <internal token>
Content-Type: application/json
```

Request:

```json
{
  "account_ref": "customer-001-tiktok",
  "title": "ONLIVEABLE Event",
  "orientation": "landscape"
}
```

Response:

```json
{
  "server_url": "rtmp://...",
  "stream_key": "...",
  "share_url": "https://...",
  "expires_at": null
}
```

### End LIVE room

```http
POST /v1/live/deactivate
```

The provider implementation can later wrap Streamlabs or another compatible TikTok LIVE integration without changing the ReLive destination model.

## Security requirements

TikTok cookies, Streamlabs tokens, signer keys and session credentials must not be:

- stored in the ReLive frontend
- returned to normal users
- committed to Git
- written to application logs
- mixed with destination telemetry

The external provider should use encrypted server-side credential storage.

## Media behavior

ReLive remains pass-through first:

```text
H.264 → COPY
AAC   → COPY
```

However, TikTok behavior and accepted stream metadata can change. The TikTok destination adapter must be allowed to use a TikTok-specific proxy/metadata layer when required without changing the main contribution channel.
