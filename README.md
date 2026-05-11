# costctl

Multi-cloud cost and pricing CLI. Today: Azure VM spot pricing (current + ~90 days of history).

- **Current prices** come from the [Azure Retail Prices API](https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices) — public, unauthenticated, current snapshot only.
- **Historical prices** come from [cloudprice.net](https://developer.cloudprice.net/)'s AzurePrice API v1 — requires a free subscription key.

## Install

```sh
go install github.com/jwmossmoz/costctl@latest
```

Or from a checkout:

```sh
make build           # produces ./costctl
make install         # installs to $GOBIN
```

## Quick start

```sh
# Current spot prices for one SKU, all regions, both OS variants:
costctl azure spot current --sku Standard_F8s_v2

# Same, filtered:
costctl azure spot current --sku Standard_F8s_v2 --region westus2 --os linux

# Historical change-points for one (SKU, region):
costctl config set-key cloudprice <YOUR_KEY>     # one-time
costctl azure spot history --sku Standard_F8s_v2 --region westus2
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
│       └── history   — cloudprice.net (key required)
└── config
    ├── set-key <provider> <key>
    ├── show
    └── path
```

Global flags: `--json`, `-v/--verbose`, `-q/--quiet`, `--no-color`, `--version`, `-h/--help`.

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
```

## Exit codes

| Code | Meaning |
| ---- | ------- |
| 0    | Success |
| 1    | Runtime error (network, API, etc.) |
| 2    | Invalid usage (handled by cobra) |

## License

MIT. See [LICENSE](LICENSE).
