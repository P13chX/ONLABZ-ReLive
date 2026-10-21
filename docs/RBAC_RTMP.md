# ReLive Access Model and RTMP Ingest

## Roles

### user

Designed for customers and operators who should not need technician-level controls.

Can:

- sign in
- see only assigned channels
- view RTMP server URL and current stream key
- copy ingest credentials
- reset/regenerate the stream key for an assigned channel
- see destination status
- start/stop destinations for assigned channels

The user UI hides detailed SRT/network graphs, bitstream diagnostics, incident telemetry and restart controls.

### administrator

Designed for experienced operators/technicians inside one customer/owner scope.

Can:

- access every channel belonging to the same owner
- create channels for that owner
- create users/administrators for that owner
- create and configure destinations
- run connection tests
- see advanced network telemetry
- see bitstream integrity
- see persistent incidents
- restart destination workers

Administrators cannot create or manage a super administrator.

### super_administrator

Global ONLIVEABLE system role.

Can:

- access every owner
- access every channel
- create channels for any owner
- create all role types
- inspect all destinations and incidents
- rotate any channel key
- operate all relay workers
- use global runtime/status administration endpoints

## Authentication

ReLive v0.7 uses server-side sessions.

Bootstrap the first super administrator with environment variables:

```bash
export RELIVE_SUPERADMIN_EMAIL="you@example.com"
export RELIVE_SUPERADMIN_PASSWORD="use-a-long-random-password"
```

No default administrator password is committed to the repository.

For HTTPS deployments:

```bash
export RELIVE_COOKIE_SECURE=true
```

Passwords are stored as salted PBKDF2-HMAC-SHA256 hashes. Session tokens are random and only SHA-256 hashes of session tokens are stored in PostgreSQL.

## RTMP-first ingest

New channels default to:

```text
Protocol: RTMP
Port:     1935
App:      /live
```

OBS configuration:

```text
Service:    Custom
Server:     rtmp://<ingest-host>:1935/live
Stream Key: rlv_<random-key>
```

The stream key is cryptographically randomized per channel.

SRT remains supported as an advanced contribution option.

## Stream-key rotation

Endpoint:

```http
POST /api/v1/channels/{id}/stream-key/reset
```

A reset:

1. generates a new random key
2. increments the channel key version
3. records the rotation time
4. stops existing destination relay processes
5. clears their process IDs
6. reconnects running destinations to the new RTMP source key
7. records a persistent incident

The previous key may still be accepted as an arbitrary RTMP publish path by datarhei Core itself, because Core uses a global RTMP token rather than per-stream key authorization.

However, ReLive no longer subscribes or relays the old path after rotation. Therefore an old key cannot continue through the ReLive distribution workflow.

## Channel scoping

Authorization is checked server-side.

- user: explicit `user_channels` assignments
- administrator: `owner_id`
- super administrator: unrestricted global scope

UI visibility is only a presentation layer and is not relied on for authorization.
