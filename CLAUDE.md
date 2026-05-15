# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this service does

TruckAPI scrapes available loads from two upstream load boards (C.H. Robinson REST API and Truckstop SOAP API), normalizes them into a `LoaderOrder` shape, and POSTs them to an internal "Loader API" (`core.hfield.net`) where dispatchers/drivers act on them. It also exposes Fiber HTTP endpoints for: CHRob webhook callbacks (offer responses, shipment details), driver-initiated offer submission and booking, and a UI feed of recent orders.

## Build / run / test

```bash
# Run from source (cds into cmd/truckapi)
./run.sh

# Or explicitly
go build -o truckapi cmd/truckapi/main.go && ./truckapi

# Docker — IMPORTANT: keep a single replica (see "CHRob dedupe" below)
docker compose up --build

# Tests
go test ./...
go test ./internal/handlers -run TestSubmitLoadOfferHandler_StrictJSONRejectsUnknownFields
```

The server listens on `SERVER_LISTEN_ADDR` (defaults: `:8081` in code, `:8080` in older README example). Static assets are served from `./public`.

## Architecture

### Entry point and runners

[cmd/truckapi/main.go](cmd/truckapi/main.go) wires everything: builds an `auth.TokenStore` + `chrobinson.APIClient`, optionally initializes the MySQL "Platform DB" (`ENABLE_PLATFORM_DB`), creates a `uifeed.Store` (capacity 2000), starts background runners (`ENABLE_CHROB` default true, `ENABLE_TRUCKSTOP` default false), and starts the Fiber app from `routes.InitializeRoutes`.

Both runners are 30-second tickers spawned as goroutines:

- [internal/chrobrunner/processor.go](internal/chrobrunner/processor.go) — fetches truck locations from `db.FetchLoaderLocations("TRUCKSTOP")` (the Loader API, not local SQL), runs a 250-mile-radius `SearchAvailableShipments` against CHRob for each, paginates with three layers of dedupe (per-location, per-cycle, 24h-recent-sent), maps via `mapShipmentToLoaderOrder`, and concurrently posts to Loader via `loader.PostPool`.
- [internal/truckstop/processor.go](internal/truckstop/processor.go) — analogous runner for the Truckstop SOAP API; iterates equipment groups (V/SPV/HS/FRV/FSDV) per location and posts to Loader.

### CHRob dedupe (critical operational invariant)

Dedupe is **in-memory only** in `chrobRecentSentCache` with a hardcoded 24h TTL. The TTL is intentionally not env-configurable — see [internal/chrobrunner/processor.go:199-204](internal/chrobrunner/processor.go#L199-L204). Restarting the process clears the cache, so old loads will be re-sent unless Loader API also dedupes.

Consequence: the service must run as exactly one instance/container. Do not scale `truckapi` in `compose.yml`. README "CHRob LoaderAPI Dedupe Deployment Notes" section documents this.

The dedupe key is `loadNumber:N` when CHRob returns one, otherwise an FNV-hash of mapped fields (origin/destination/dates/equipment/weight/miles) — see `chrobDedupKey`.

### CHRob client + token lifecycle

[internal/auth/auth.go](internal/auth/auth.go) holds a `TokenStore` keyed off `CHROB_ACCESS_TOKEN`. On startup it parses the JWT `exp` claim from any token already in `.env`; if missing/expired/non-JWT it forces a refresh on first use. After refresh it calls `config.SaveEnv(".env")`, which **rewrites the entire `.env` file** with all keys in `config.EnvKeys` — be aware that running locally will mutate `.env`.

[internal/chrobinson/api.go](internal/chrobinson/api.go) exposes `HandleAPICall(client, fn)`: runs `fn`, and if the upstream returns 401, refreshes the token and retries once. Use this wrapper for any CHRob call.

`internal/chrobinson/runtime_costs.go` caches `AvailableLoadCosts` per loadNumber (LRU, 500 entries) so booking calls can reconstruct the exact cost shape CHRob expects without re-querying.

### HTTP layer (Fiber)

[internal/routes/routes.go](internal/routes/routes.go) is the canonical list of routes. Highlights:

- Callbacks `POST /offerResponse/callback/here` and `POST /shipmentDetails/callback/here` are gated by [`middlewares.OfferCallbackAuthMiddleware`](internal/middlewares/middlewares.go) — bearer token (`CHROB_CALLBACK_BEARER_TOKEN`) is required; `X-API-KEY` fallback only when `CHROB_CALLBACK_ALLOW_API_KEY=true`. If no bearer is configured, the middleware returns 503 (fail-closed).
- `POST /v1/shipments/:loadNumber/offers` accepts an `Idempotency-Key` header; replay logic and request fingerprinting live in [internal/handlers/offers_helpers.go](internal/handlers/offers_helpers.go) (`offerSubmitIdempotency`). TTL from `BID_IDEMPOTENCY_TTL_MINUTES` (default 60).
- Offer / booking / shipment-detail records are kept in an in-process `runtimeStore` ([internal/handlers/runtime_store.go](internal/handlers/runtime_store.go)) — capacity 500, newest-first, **lost on restart**. There is no SQLite anymore (removed in commit `c99316b`).
- When an offer-response callback arrives with a stored `OrderBidID`, the handler forwards a `BrokerResponse` to Loader's `/api/v1/loader/order-bids/broker-response` exactly once and stamps `BrokerResponseAt` to make the action idempotent.
- `OfferResponseHandler` translates CHRob `OfferResult` → internal status: `Accepted→booked`, `Rejected|NotConsidered→declined`, `Counter→countered`.

### Loader API client + post pool

[internal/loader/client.go](internal/loader/client.go) wraps the downstream Loader API. `LOADER_ORDERS_BASE_URL` overrides `LOADER_API_BASE_URL` for *order POSTs only* — Truckstop intentionally bypasses this override so the override can route CHRob orders to the local mock receiver (see `truckstop/processor.go` comment). `PostPool` runs configurable workers (default 16) with exponential-backoff retries for 429/5xx and network errors, and treats 2xx responses with `{"success":false}` / `{"ok":false}` / `{"status":"error"}` as failures.

### Mock loader (local dup testing)

[internal/mockloader/store.go](internal/mockloader/store.go) + [internal/handlers/mockloader.go](internal/handlers/mockloader.go) host a Loader replica at `/debug/mock-loader/*`. Point `LOADER_ORDERS_BASE_URL=http://127.0.0.1:8081/debug/mock-loader` to capture/duplicate-check what CHRob posts would have gone out as.

### Optional MySQL "Platform DB"

[db/db.go](db/db.go) connects to a MySQL `platform` DB (DSN is **hardcoded** at line 40) only when `ENABLE_PLATFORM_DB` is truthy. It is independent of the CHRob runtime and is used by `CombinedShipmentInfoHandler` and the WebSocket handler to query `pseudo_locations` joined with trucks/drivers/dispatchers. Most flows do not need it.

### Config and logging

[pkg/config/config.go](pkg/config/config.go) loads env from `.env`, overlaid by process env. On `init()` it `os.Chdir`s to the directory containing `go.mod`, so working directory is always project root regardless of where the binary is launched. `SaveEnv` rewrites `.env` from the `EnvKeys` list. Logging is logrus JSON to `stderr` + rotating file at `./logs/app.log` (lumberjack: 25MB / 10 backups / 14d, all overridable via `LOG_FILE`, `LOG_MAX_SIZE_MB`, `LOG_MAX_BACKUPS`, `LOG_MAX_AGE_DAYS`, `LOG_COMPRESS`, `LOG_LEVEL`).

## Required environment

Beyond what's in README, additional keys live in [pkg/config/config.go](pkg/config/config.go):

- `CHROB_API_BASE_URL` — defaults to `https://api.navisphere.com` (production); `main.go` warns when this points to prod and infers `CHROB_ENV` (`sandbox`/`production`/`custom`) from the URL when not explicitly set.
- `CHROB_CARRIER_CODE` — defaulted in `DefaultEnvValues`; auto-injected into search/offer/booking requests when callers omit it.
- `LOADER_API_BASE_URL`, `LOADER_API_KEY`, `LOADER_ORDERS_BASE_URL` — see "Loader API client" above.
- `LOADER_POST_WORKERS`, `LOADER_POST_MAX_RETRIES` — pool tuning.
- `LOADER_LOG_SUCCESS_BODY=true` — log full Loader 2xx response bodies (off by default; verbose).
- `ENABLE_LOADER_POST`, `ENABLE_UI_FEED` — gate runner output paths independently.
- `BID_IDEMPOTENCY_TTL_MINUTES` — offer submit idempotency window.
- `OPENAI_API_KEY` — only used by `db/apiqueries.go` `getCityDetails` (currently dead code path; safe to leave unset).

## Conventions worth preserving

- All CHRob calls go through `chrobinson.HandleAPICall` so the 401-refresh-and-retry happens in one place.
- `pageNow := time.Now()` is captured *once per page* in the runner so all dedupe marks within a page share a timestamp — keep this pattern.
- Don't add per-order success logs at info level in the loader post path; they're explicitly demoted to debug because of volume (`internal/loader/client.go`).
- The `recentKeyCache` TTL setter is idempotent and bounded; if you tune dedupe, do it through `chrobRecentSentCache.SetTTL` rather than introducing parallel caches.
- `internal/handlers/runtime_store.go` treats slices as newest-first bounded queues; preserve this when adding new in-memory record types.
