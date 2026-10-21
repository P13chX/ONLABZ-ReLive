# ReLive Operations UI

## Overview input preview

ReLive v0.8 creates a monitor-only preview process for the selected channel.

Media path:

```text
RTMP / SRT publisher
      │
      ├── production relay workers -> destinations
      │
      └── preview worker
            -c:v copy
            -c:a copy
            HLS -> Core memfs
                  │
                  ▼
            /preview/*.m3u8
                  │
                  ▼
            Technician browser
```

The preview process does not sit in the destination relay path and does not modify the production stream.

The browser uses native HLS when available and a pinned hls.js fallback for browsers that require Media Source Extensions.

## Audio meter

Preview starts muted.

When the operator selects **Enable audio**, ReLive uses the browser Web Audio API to analyze the actual audio samples from the preview media element and displays an RMS dBFS meter.

This meter is monitoring-only. It does not add an audio filter or decode stage to the production relay workers.

## Live source panel

The source media panel reads datarhei Core FFmpeg process input progress and displays:

- video codec
- width / height
- FPS
- video bitrate
- audio codec
- audio bitrate
- audio sample rate
- audio channel count

These values describe the actual source observed by the preview process rather than the saved channel profile.

## Preview security

HLS manifests and segments are not exposed as a public unauthenticated Core path.

nginx uses `auth_request` for every `/preview/` request. ReLive validates the current session and extracts the channel ID from the preview filename, then applies the same server-side RBAC rules used by the API.

## Super Administrator

The sidebar exposes four global management screens only to `super_administrator`.

### Owners

- list owner scopes
- create owner
- enable / disable owner
- show channel count
- show user count

### Users

- list all users
- create user
- create administrator
- create super administrator
- change role
- enable / disable account
- server protects the last active super administrator

### Channels

- list all channels across owners
- create RTMP/SRT channel
- reset stream key
- delete channel
- backend also exposes PATCH for channel configuration

### System Health

Shows current:

- ReLive overall state
- PostgreSQL state
- datarhei Core state
- owner / user / channel counts
- destination count
- live / failed destination counts
- RTMP publisher count
- SRT publisher count
- active preview process count
- critical incidents in the last hour

## Operational note

The preview worker uses stream copy and therefore expects browser-compatible source media for direct preview. The standard ReLive source profile remains H.264 video + AAC audio.

A future preview-only transcode fallback may be useful for HEVC/AV1 inputs, but it should remain isolated from the production relay path.
