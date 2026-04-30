# grafana-splunk-datasource

A [Grafana](https://grafana.com) data source plugin for [Splunk](https://splunk.com). Query logs via the Splunk REST API and render them in Grafana's Logs panel and Explore.

Designed and tested against **Splunk Cloud**, but works against any Splunk REST API endpoint (on-prem, BYOL, etc) — it's all driven by the URL you configure.

## Status

Early. Supports synchronous SPL searches via `services/search/jobs/export` and renders results as Grafana log frames. Async/long-running searches, saved searches, and metric queries (`mstats`) aren't wired up yet — see [Roadmap](#roadmap).

## Requirements

- Grafana **10.4** or newer.
- A Splunk deployment reachable from Grafana over HTTPS.
- A Splunk **authentication token** (NOT a HEC token — those are for ingest only).

## Getting a Splunk auth token

**Splunk Cloud Platform**: *Settings → Tokens → New Token*. The token is bound to a user — pick a service account with read access to the indexes you want to query. Copy the token immediately; you can't view it again. ([docs](https://docs.splunk.com/Documentation/Splunk/latest/Security/UseAuthTokens))

**Splunk Enterprise (on-prem)**: same path. You may need to enable token auth first via *Settings → Tokens → Enable Token Authentication*.

> **Important — Splunk Cloud network access**
> Splunk Cloud doesn't expose port 8089 publicly by default. You typically need to file a support ticket to get your Grafana host added to the IP allowlist for the management port, or you'll get connection timeouts. Check the *Admin Config Service (ACS)* docs for the self-service path on newer stacks.

## Quickstart (local dev)

```bash
git clone https://github.com/firestoned/grafana-splunk-datasource.git
cd grafana-splunk-datasource

# Frontend
npm install
npm run build           # one-shot build into ./dist
# or: npm run dev       # watch mode

# Backend
go mod download
go install github.com/magefile/mage@v1.15.0
mage -v buildAll        # builds gpx_splunk binaries into ./dist

# Run Grafana with the plugin mounted
export SPLUNK_URL="https://abc.splunkcloud.com:8089"
export SPLUNK_TOKEN="eyJraWQiOiJzcGx1bmsuc2VjcmV0..."
docker compose up
```

Open http://localhost:3000, log in as `admin` / `admin`. The Splunk data source is auto-provisioned from the env vars. You can also configure it manually under **Connections → Data sources**.

## Project layout

```
.
├── src/                       # TypeScript frontend
│   ├── plugin.json            # logs: true, backend: true, id, type, etc.
│   ├── module.ts              # Entry — registers DataSource + editors
│   ├── datasource.ts          # DataSourceWithBackend subclass
│   ├── types.ts               # Query and config types
│   └── components/
│       ├── ConfigEditor.tsx   # URL + auth token + TLS skip
│       └── QueryEditor.tsx    # SPL editor, max results, time overrides
│
├── pkg/                       # Go backend
│   ├── main.go                # datasource.Manage(...) entry
│   └── plugin/
│       ├── datasource.go      # QueryData (export streaming) + CheckHealth
│       └── datasource_test.go # Tests against an httptest fake Splunk
│
├── provisioning/datasources/  # Auto-config for local dev
├── docker-compose.yaml        # Local Grafana with plugin mounted
├── Magefile.go                # Backend build entry
├── go.mod                     # Go module
└── package.json               # Frontend deps and scripts
```

## How it works

The plugin is `"backend": true` and `"logs": true`, so:

1. The browser sends a query (just SPL + a time range) to the Grafana server.
2. Grafana forwards it via gRPC to the Go binary (`gpx_splunk`).
3. The Go process holds the decrypted auth token and calls `POST /services/search/jobs/export?output_mode=json` on Splunk. This endpoint is **synchronous and streaming** — Splunk runs the search and writes results back as JSONL on the same response, no polling needed.
4. The backend parses each line, builds a `data.Frame` tagged as `FrameTypeLogLines`, and returns it.
5. Grafana renders the frame in the Logs panel — expandable lines, severity colors, label filters, the works.

The token never reaches the browser (it's in `secureJsonData`, encrypted at rest). All HTTP to Splunk is server-to-server, so CORS is a non-issue.

## Configuring a data source

| Field            | What                                                                                          |
| ---------------- | --------------------------------------------------------------------------------------------- |
| URL              | Splunk REST API base. SaaS: `https://<stack>.splunkcloud.com:8089`. On-prem: `https://<host>:8089`. |
| Auth Token       | Splunk authentication token. Sent as `Authorization: Bearer <token>`.                         |
| Skip TLS Verify  | Off by default. Only enable for self-signed certs in dev.                                     |

**Save & Test** runs `CheckHealth`, which calls `GET /services/server/info` and reports auth failures distinctly (401, 403, network).

## Writing queries

The query editor accepts raw SPL. The `search ` command is implicit — `index=main level=ERROR` and `search index=main level=ERROR` are equivalent. Pipe-prefixed queries (`| stats count by host`) are passed through unchanged.

| Field         | What                                                                                     | Example                          |
| ------------- | ---------------------------------------------------------------------------------------- | -------------------------------- |
| SPL           | Required. Multi-line SPL.                                                                | `index=main level=ERROR`         |
| Max results   | Splunk `count` parameter. Default 1000. 0 = unlimited (be careful).                       | `500`                            |
| Earliest      | Optional override. Splunk relative or absolute time.                                     | `-15m`, `-1h@h`, `2024-04-01`    |
| Latest        | Optional override.                                                                       | `now`                            |

If `Earliest` / `Latest` are blank, the panel's time range is used (passed as RFC3339 to Splunk, which accepts it).

### Examples

Show all errors in the last hour from a specific app:

```
index=app sourcetype=app:json level=ERROR
```

Top 10 hosts emitting auth failures:

```
index=auth action=failure | top 10 host
```

Tail logs by host (useful with Grafana's live mode):

```
index=main host=web-01
```

## Returned frame shape

Each result frame has these columns:

| Field        | Type      | Source             |
| ------------ | --------- | ------------------ |
| `timestamp`  | `time.Time` | `_time`          |
| `body`       | `string`  | `_raw`             |
| `host`       | `string`  | `host`             |
| `source`     | `string`  | `source`           |
| `sourcetype` | `string`  | `sourcetype`       |
| `index`      | `string`  | `index`            |

Frame meta is set to `Type: FrameTypeLogLines` and `PreferredVisualization: VisTypeLogs` so the Logs panel and Explore's Logs view render it correctly.

## Testing

```bash
npm run test:ci   # frontend Jest
go test ./...     # backend unit tests against an httptest fake Splunk
```

## Building a release artifact

Grafana plugins are distributed as a zip of the built `dist/` directory, named after the plugin id:

```bash
npm run build           # frontend → dist/
mage -v buildAll        # backend  → dist/gpx_splunk_*
npm run package         # → firestoned-splunk-datasource-<version>.zip
```

The zip contains a single top-level directory `firestoned-splunk-datasource/` — the layout Grafana expects under its plugins directory.

## Installing the plugin

Releases are **unsigned** — this plugin targets OSS / on-prem deployments. Grafana refuses to load unsigned plugins by default, so every install method below has two parts:

1. Allow the unsigned plugin to load (one-time per Grafana instance).
2. Get the plugin zip onto disk.

### Step 1 — Allow the unsigned plugin to load

The plugin id is `firestoned-splunk-datasource`. Apply **one** of the following — whichever matches how Grafana is deployed.

**Environment variable** (Docker, systemd, Kubernetes — most setups):

```
GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=firestoned-splunk-datasource
```

Comma-separate to allow multiple:

```
GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=firestoned-splunk-datasource,firestoned-dynatrace-datasource
```

**`grafana.ini`** (deb/rpm package installs — `/etc/grafana/grafana.ini`):

```ini
[plugins]
allow_loading_unsigned_plugins = firestoned-splunk-datasource
```

**Docker Compose**:

```yaml
services:
  grafana:
    image: grafana/grafana-oss:11.1.0
    environment:
      - GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=firestoned-splunk-datasource
```

**Kubernetes — Helm `grafana/grafana` chart** (`values.yaml`):

```yaml
env:
  GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS: firestoned-splunk-datasource
```

**systemd unit override** (running `grafana-server` directly):

```bash
sudo systemctl edit grafana-server
```

Add:

```ini
[Service]
Environment="GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=firestoned-splunk-datasource"
```

Then `sudo systemctl daemon-reload && sudo systemctl restart grafana-server`.

**Verifying it took effect** — after Grafana restarts, the log should contain a line like:

```
logger=plugin.loader level=warn msg="Permitting unsigned plugin. This is not recommended" pluginID=firestoned-splunk-datasource
```

(Default log path: `/var/log/grafana/grafana.log`.) If you don't see that line, the env var or `grafana.ini` setting isn't being read by the running process.

### Step 2 — Install the plugin zip

Three methods. Pick whichever matches your environment.

#### Manual install (any Grafana, including Docker)

Unzip into Grafana's plugins directory and restart:

```bash
unzip firestoned-splunk-datasource-<version>.zip -d /var/lib/grafana/plugins/
systemctl restart grafana-server
```

Default plugin path is `/var/lib/grafana/plugins` (deb/rpm) or `/opt/homebrew/var/lib/grafana/plugins` (Homebrew). Override with `GF_PATHS_PLUGINS`.

#### `grafana-cli` from a URL

Host the zip somewhere reachable (GitHub Releases, S3, internal mirror):

```bash
grafana-cli --pluginUrl https://example.com/firestoned-splunk-datasource-<version>.zip \
  plugins install firestoned-splunk-datasource
```

#### Grafana Docker image (`GF_INSTALL_PLUGINS`)

The official `grafana/grafana` image installs plugins on startup from `GF_INSTALL_PLUGINS` — use the `url;id` form for a custom zip, paired with the allowlist env var from Step 1:

```yaml
environment:
  - GF_INSTALL_PLUGINS=https://example.com/firestoned-splunk-datasource-<version>.zip;firestoned-splunk-datasource
  - GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=firestoned-splunk-datasource
```

After install, restart Grafana and confirm the plugin appears under **Administration → Plugins**.

## Roadmap

- Token via `Authorization: Splunk <token>` as a fallback for older deployments.
- Username/password auth (session-based) for legacy on-prem.
- Basic SPL syntax highlighting in the query editor (custom Monaco language).
- Saved-search picker (calls `/services/saved/searches`).
- Async job mode for long-running searches with progress indicator.
- `mstats` / metrics queries — would set `metrics: true` and return a different frame shape.
- Variable support (`metricFindQuery`) for dropdown variables driven by SPL.

## License

Apache-2.0. See [LICENSE](LICENSE).
