# Bitstream Integrity

ReLive is pass-through-first. The normal production path must not silently lower source quality.

## Policy

The default relay path is:

```text
OBS / encoder
  -> SRT contribution
  -> datarhei Core
  -> internal SRT subscriber
  -> independent destination worker
  -> -c:v copy -c:a copy
  -> destination
```

Transcoding must never be enabled implicitly.

## Runtime integrity states

Each destination exposes:

- `preserved` — source and output codec, resolution, FPS and audio format match.
- `changed` — at least one quality-significant field changed.
- `unknown` — the process has not produced enough input/output telemetry yet.

The current comparison checks:

- video codec
- resolution
- frame rate, with 0.5 FPS tolerance
- audio stream presence
- audio codec
- audio sample rate
- audio channel count

Bitrate is displayed for comparison but is not used alone to mark a stream changed because live mux/progress measurements naturally vary.

## Technician Console

Destination rows show:

```text
PRESERVED
```

or:

```text
QUALITY CHANGED
```

with source -> output detail.

A quality transition to `changed` creates a critical persistent incident. Returning to `preserved` creates a recovery incident.

## Why this exists

Upstream Restreamer can use an ingest/transcoding profile before an egress that itself says `copy`. In that architecture, the destination may copy a stream that has already been re-encoded.

ReLive avoids that production path and subscribes directly to the published internal SRT resource.

## Upstream issues considered

Relevant upstream reports include:

- datarhei/restreamer #787 — incorrect/duplicate FFmpeg filter construction.
- datarhei/restreamer #943 — conflicting frame-rate passthrough arguments.
- datarhei/restreamer #728 — YouTube audio stutter regression.
- datarhei/restreamer #552 — passthrough/HDR forwarding concerns.

These upstream reports are not automatically assumed to affect every ReLive deployment. They are treated as regression cases worth guarding against.

## Mandatory local check

Before merging a ReLive API change:

```bash
sh scripts/premerge-check.sh
```

This runs:

- `gofmt -l .`
- `go test ./...`
- `go vet ./...`
- `go build ./...`

This check is local and does not consume GitHub Actions quota.
