# Agent guide for costctl

This file is the source of truth for AI coding agents working in this repo.
Read it before making changes.

## What this is

A small Go CLI that wraps cloud price catalogs. Today it handles Azure VM spot
pricing — current snapshots from the Azure Retail Prices API and ~90-day history
from cloudprice.net. The package layout assumes more clouds (AWS, GCP) will land
later as siblings of the `azure` command tree.

## Layout

```
main.go                      # thin entrypoint, just calls cmd.Execute()
cmd/                         # cobra command tree
  root.go                    # rootCmd + persistent global flags
  config.go                  # `costctl config ...`
  azure.go                   # `costctl azure spot {current,history}`
internal/
  config/                    # JSON config loader (XDG path, 0600 perms)
  cloudprice/                # client for data.cloudprice.net/api/v1/...
  azureretail/               # client for prices.azure.com/api/retail/prices
```

Keep `internal/` packages free of cobra and any CLI concerns — they are plain
clients and should remain reusable from a library context.

## Build / test

```sh
make build        # produces ./costctl with version stamped from `git describe`
make test         # go test ./...
make vet          # go vet ./...
make fmt          # gofmt -w -s .
```

Go 1.22+ is required. The `Makefile` injects `cmd.Version` via `-ldflags`.

## CLI conventions

- All primary output goes to **stdout**; diagnostics and progress to **stderr**.
- `--json` switches stdout to machine output without changing stderr.
- `-q/--quiet` suppresses progress lines; `-v/--verbose` adds context (e.g. where
  the API key was resolved from).
- Error messages on stderr are prefixed with `error:` by `cmd.Execute()`.
- Exit codes: `0` success, `1` runtime error, `2` invalid usage (cobra-enforced).
- Use cobra's `Example:` field with a multi-line `strings.TrimSpace(...)` block
  rather than ad-hoc `Long:` examples.

## API key handling

Resolution precedence is **flag > env > config file** and is implemented in
`internal/config.ResolveAPIKey`. Do not bypass it — even in tests.

- Config path: `$XDG_CONFIG_HOME/costctl/config.json` (default
  `~/.config/costctl/config.json`), mode `0600`, parent dir `0700`.
- Override the path with `COSTCTL_CONFIG=<path>` for tests.
- Keys are namespaced under `providers.<name>` so additional providers slot in
  without breaking the file shape.
- `costctl config show` and `--json` output **must** mask keys via `maskKey()`.
  Never print a raw key.

## Upstream API contracts

Capturing what we learned the hard way so future agents don't have to retrace.

### cloudprice.net (`data.cloudprice.net`)

- Host is `data.cloudprice.net` — **not** `developer.cloudprice.net` (that's the
  SPA portal; the gateway is on a different subdomain).
- Auth: `?subscription-key=<KEY>` query parameter. The `Ocp-Apim-Subscription-Key`
  header also works but the query form is what the portal documents.
- Endpoint we use: `GET /api/v1/price_history_vm?vmname=<sku>&tier=<spot|standard|low>&regions=<region>`.
- `regions=` accepts **one** region. Comma-separated values return an empty list.
  Singular `region=` is silently ignored. To query multiple regions, loop.
- `fromDate` / `toDate` parameters are accepted but ignored — the server caps the
  window to ~90 days regardless.
- Response: newest entries first. Our client returns them as-is; the cobra layer
  reverses to oldest-first for human rendering.
- 401 → `cloudprice.ErrUnauthorized`. 404 → `cloudprice.ErrNotFound`.
- The batch-export product (`/batch/azure/*.gz`) is a **paid add-on** and 401s
  with a standard subscription key. We do not target it.

### Azure Retail Prices (`prices.azure.com`)

- Public, unauthenticated. No history — current effective price only.
- Spot meters live in `serviceName='Virtual Machines'`, `priceType='Consumption'`,
  with `meterName` containing `'Spot'`. Linux and Windows are separate meters
  distinguished by `productName` ("... Windows" suffix on the Windows variant).
- Pagination: each page has up to 1000 items and a `NextPageLink`. The client
  follows the link until empty.

## Adding a new cloud

1. Add `internal/<cloud>/client.go` — keep it cobra-free.
2. Add `cmd/<cloud>.go` registering a sibling command (`costctl aws ...`).
3. If it needs credentials, reuse `cfg.ResolveAPIKey` with a new provider name
   constant. Do not invent a new resolution path.
4. Update `README.md` and the command tree at the top of this file.

## Homebrew tap

`brew install jwmossmoz/tap/costctl` is wired up via the `brews:` block in
`.goreleaser.yml`. On every `v*` tag push, goreleaser commits an updated
Formula to `jwmossmoz/homebrew-tap`. The cross-repo write needs a PAT with
`contents: write` on the tap repo, stored as the `HOMEBREW_TAP_TOKEN` secret on
this repo (the default `GITHUB_TOKEN` can't write to other repos).

goreleaser emits a deprecation warning that `brews` is being phased out in
favor of `homebrew_casks`. **Don't switch yet.** Casks set macOS quarantine
attributes on downloaded binaries, which would block unsigned binaries at
runtime. Switching requires notarization (Apple Developer ID + `notarytool`)
— if/when we invest in that, swap `brews:` for `homebrew_casks:`.

## Things we intentionally did NOT do

- **No concurrent fetches.** History queries are sequential because cloudprice
  has no documented rate limit and the data volume is small. If we later need
  parallelism, add it behind a `--concurrency` flag — don't make it the default.
- **No on-disk caching.** Prices change daily; staleness would surprise users.
- **No `viper`.** The config is small and JSON is sufficient. Adding viper would
  pull in YAML/TOML/etcd dependencies for no gain.
- **No third-party color libs.** If we add color later, gate it on
  `os.Stdout` being a TTY and respect `NO_COLOR` and `--no-color`.
- **No analytics / telemetry.** Ever.

## Style

- Run `gofmt -s` and `go vet ./...` before committing.
- Comments explain **why**, not what. The exception is package-level doc
  comments, which should orient a reader unfamiliar with the package.
- Error messages: lowercase, no trailing punctuation, wrap with `%w` when the
  caller may want to test with `errors.Is`.
- Keep dependencies minimal. Today: `spf13/cobra` only. Discuss before adding
  another.
