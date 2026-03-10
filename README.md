# resleased - Resource Lease Daemon

A lightweight HTTP service for exclusive, time-bounded resource reservations.
Designed for CI/CD pipelines and test frameworks that share hardware or services
that can only be used by one job at a time.

---

## Concept

| Term | Meaning |
|------|---------|
| **Resource** | Any unique ID (e.g. `X1`, `jtag-board-3`, `gpu-node-42`) |
| **Lease** | An exclusive, time-bounded reservation of a resource |
| **Token** | A secret returned on successful reservation - needed to extend or release |

---

## Build & Run

```bash
# Generate openapi docs
go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/resleased/main.go -o docs/

go build -o resleased ./cmd/resleased

# defaults: listen :8080, state file ./resleased.json
./resleased

# custom options
./resleased -addr :9090 -state /var/lib/resleased/state.json -purge-interval 1m
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | HTTP listen address |
| `-state` | `resleased.json` | Path to JSON state file |
| `-purge-interval` | `5m` | How often to clean expired leases from disk |

---

## API

### Reserve a resource

```
POST /api/v1/reserve
```

```json
{ "resource_id": "X1", "owner": "ci-job-42", "duration": "2h" }
```

**Duration format**: Go duration strings - `30m`, `2h`, `1h30m`, `90s`.

**200 OK** - resource is yours:
```json
{ "token": "a3f9…", "expires_at": "2026-03-10T15:04:05Z" }
```

**503 Service Unavailable** - resource is taken:
```json
{
  "error": "resource locked",
  "owner": "ci-job-17",
  "reserved_until": "2026-03-10T14:00:00Z",
  "remaining_seconds": 3547
}
```

---

### Extend a reservation

```
POST /api/v1/extend
```

```json
{ "token": "a3f9…", "duration": "1h" }
```

**200 OK**:
```json
{ "expires_at": "2026-03-10T16:04:05Z" }
```

**404** - unknown or expired token.

---

### Release a reservation

```
DELETE /api/v1/release
```

```json
{ "token": "a3f9…" }
```

**200 OK**:
```json
{ "released": true }
```

---

### Check resource status

```
GET /api/v1/status/{resource_id}
```

**200 OK** (available):
```json
{ "available": true }
```

**200 OK** (locked):
```json
{
  "available": false,
  "owner": "ci-job-42",
  "reserved_until": "2026-03-10T15:04:05Z",
  "remaining_seconds": 3547
}
```

---

## State file

State is persisted to a JSON file so leases survive restarts.
All times are stored as **RFC3339 UTC timestamps** - no wall-clock tracking needed.
On startup, expired leases are silently discarded.

Example `resleased.json`:
```json
{
  "reservations": {
    "X1": {
      "resource_id": "X1",
      "owner": "ci-job-42",
      "token": "a3f9c1d2e4b5...",
      "expires_at": "2026-03-10T15:04:05Z",
      "created_at": "2026-03-10T13:04:05Z"
    }
  }
}
```

---

## Usage in a GitHub Actions / CI pipeline

```yaml
- name: Reserve hardware resource
  id: lease
  run: |
    RESP=$(curl -sf -X POST http://resleased:8080/api/v1/reserve \
      -H 'Content-Type: application/json' \
      -d '{"resource_id":"jtag-board-1","owner":"${{ github.run_id }}","duration":"1h"}')
    echo "token=$(echo $RESP | jq -r .token)" >> $GITHUB_OUTPUT

- name: Run tests
  run: make test-hardware

- name: Release resource
  if: always()
  run: |
    curl -sf -X DELETE http://resleased:8080/api/v1/release \
      -H 'Content-Type: application/json' \
      -d '{"token":"${{ steps.lease.outputs.token }}"}'
```

---

## Compose

```yaml
services:
  resleased:
    image: axi92/resleased:latest
    container_name: resleased
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    #command:
    #  - -addr=:8080
    #  - -state=/data/state.json
    #  - -purge-interval=5m
```

---

## Systemd unit

```ini
[Unit]
Description=resleased resource lease daemon
After=network.target

[Service]
ExecStart=/usr/local/bin/resleased -addr :8080 -state /var/lib/resleased/state.json
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```
