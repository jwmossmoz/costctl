# costctl

Multi-cloud cost and pricing CLI. Today: **Azure** and **GCP** spot pricing (current + ~90 days of history).

- **Azure current** — [Azure Retail Prices API](https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices) (unauthenticated, current snapshot only).
- **Azure history** — [cloudprice.net](https://developer.cloudprice.net/) AzurePrice API v1 (requires subscription key).
- **GCP current + history** — cloudprice.net CloudPrice API v2 (same key). No native Google API is used today; if and when GCP exposes preemptible/spot price history publicly, we'll prefer it.

## Install

Homebrew (macOS / Linux):

```sh
brew install jwmossmoz/tap/costctl
```

Go toolchain:

```sh
go install github.com/jwmossmoz/costctl@latest
```

Prebuilt binaries for Linux / macOS / Windows are attached to each
[release](https://github.com/jwmossmoz/costctl/releases).

From a checkout:

```sh
make build           # produces ./costctl
make install         # installs to $GOBIN
```

## Quick start

```sh
# Store your cloudprice.net key once (used for both Azure history + all GCP):
costctl config set-key cloudprice <YOUR_KEY>

# Azure current spot prices (no key required for this one):
costctl azure spot current --sku Standard_F8s_v2 --region westus2 --os linux

# Azure ~90-day history:
costctl azure spot history --sku Standard_F8s_v2 --region westus2

# GCP current spot prices across all regions:
costctl gcp spot current --machine-type n2-standard-2

# GCP ~90-day history (region is required upstream):
costctl gcp spot history --machine-type n2-standard-2 --region us-central1

# Worker pool prices from fxci-config / tc-admin output:
uv run python tc-admin.py generate --environment=firefoxci --resources worker_pools \
  --grep 'gecko-t/win11-64-25h2-amd' --json | costctl worker-pool
```

## Configuration

API keys are resolved with this precedence:

1. `--api-key` flag
2. `CLOUDPRICE_API_KEY` env var
3. config file (`$XDG_CONFIG_HOME/costctl/config.json`, default `~/.config/costctl/config.json`)

The config file is written with mode `0600` and namespaces keys by provider so other clouds (AWS, GCP, …) can land later without reshape.

```sh
costctl config set-key cloudprice XXXXXXXX     # persist
costctl config show                            # masked
costctl config path                            # print resolved file path
```

Override the file location with `COSTCTL_CONFIG=/path/to/config.json`.

## Commands

```
costctl
├── azure
│   └── spot
│       ├── current   — Azure Retail Prices (no key required)
│       └── history   — cloudprice.net AzurePrice v1 (key required)
├── gcp
│   └── spot
│       ├── current   — cloudprice.net CloudPrice v2 (key required)
│       └── history   — cloudprice.net CloudPrice v2 (key required)
├── cache
│   ├── show          — print cache path + size
│   └── clear         — drop all cached responses
├── worker-pool       — current prices for a tc-admin generated worker pool
└── config
    ├── set-key <provider> <key>
    ├── show
    └── path
```

Global flags: `--json`, `-v/--verbose`, `-q/--quiet`, `--no-color`, `--no-cache`, `--cache-ttl 24h`, `--version`, `-h/--help`.

## Caching

Successful cloudprice responses are cached on disk at
`$XDG_CACHE_HOME/costctl/` (default `~/.cache/costctl/`) for 24h by default
— cloudprice itself updates once a day, so this is safe.
Override with `--cache-ttl 1h`, bypass for one run with `--no-cache`, or set
`COSTCTL_CACHE_DIR=<path>` to relocate.

429 rate-limit responses are retried transparently with exponential backoff.
CloudPrice retries honor `Retry-After`; Azure Retail retries honor
`x-ms-ratelimit-microsoft.consumption-retry-after` and `Retry-After`. Azure
Retail 503 responses are retried the same way.

## Examples

```sh
# Plain-text current spot prices:
costctl azure spot current --sku Standard_D32ads_v5

# JSON for downstream tools:
costctl azure spot current --sku Standard_D32ads_v5 --json | jq

# Spot history in eastus2 for Windows only:
costctl azure spot history --sku Standard_E8ads_v6 --region eastus2 --os windows

# Pipe-friendly: feed several SKUs from a file
xargs -I {} costctl azure spot history --sku {} --region westus2 --json < skus.txt

# Use fxci-config as the worker-pool source of truth:
uv run python tc-admin.py generate --environment=firefoxci --resources worker_pools \
  --grep 'gecko-t/win11-64-25h2-amd' --json | costctl worker-pool --configured-only

# Or generate once and select a pool from a larger tc-admin JSON file:
costctl worker-pool gecko-t/win11-64-25h2-amd --from /tmp/worker-pools.json --json
```

`worker-pool` reads tc-admin generated JSON from stdin by default. For Azure
worker pools it extracts `armDeployment.parameters.vmSize` and `location`, then
queries Azure Retail current spot prices across all available regions. For GCP
worker pools it extracts the generated Compute Engine `machineType` and `region`,
then queries cloudprice.net current prices across all available regions. The
configured worker-pool regions are marked in the output; add `--configured-only`
to show just those regions. Azure `--os` defaults to `any` because some FXCI
Windows ARM pools do not have Windows-labelled Retail Prices meters.

## Exit codes

| Code | Meaning |
| ---- | ------- |
| 0    | Success |
| 1    | Runtime error (network, API, etc.) |
| 2    | Invalid usage (handled by cobra) |

## License

MIT. See [LICENSE](LICENSE).
