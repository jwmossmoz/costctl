package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jwmossmoz/costctl/internal/azureretail"
	"github.com/jwmossmoz/costctl/internal/cloudprice"
	"github.com/jwmossmoz/costctl/internal/tcadmin"
)

type azureRetailSpotClient interface {
	SpotPrices(ctx context.Context, armSkuName string) ([]azureretail.Item, error)
}

type gcpCurrentClient interface {
	GCPCurrent(ctx context.Context, machineType string) (*cloudprice.GCPCurrentResponse, error)
}

var (
	flagWorkerPoolFrom           string
	flagWorkerPoolOS             string
	flagWorkerPoolConfiguredOnly bool
	flagWorkerPoolAPIKey         string

	newAzureRetailSpotClient = func() azureRetailSpotClient { return azureretail.New() }
	newWorkerPoolGCPClient   = func(key string) gcpCurrentClient { return newCloudpriceClient(key) }
)

var workerPoolCmd = &cobra.Command{
	Use:   "worker-pool [worker-pool-id]",
	Short: "Current spot prices for a tc-admin generated worker pool",
	Example: strings.TrimSpace(`
  uv run python tc-admin.py generate --environment=firefoxci --resources worker_pools \
    --grep 'gecko-t/win11-64-25h2-amd' --json | costctl worker-pool

  uv run python tc-admin.py generate --environment=firefoxci --resources worker_pools --json | \
    costctl worker-pool gecko-t/win11-64-25h2-amd --json

  costctl worker-pool gecko-t/win11-64-25h2-amd --from /tmp/worker-pools.json --configured-only`),
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkerPool,
}

type workerPoolPriceReport struct {
	WorkerPoolID      string               `json:"worker_pool_id"`
	Source            string               `json:"source"`
	Cloud             string               `json:"cloud"`
	ProviderID        string               `json:"provider_id"`
	OSFilter          string               `json:"os_filter,omitempty"`
	ConfiguredRegions []string             `json:"configured_regions"`
	SKUs              []string             `json:"skus,omitempty"`
	MachineTypes      []string             `json:"machine_types,omitempty"`
	Prices            []workerPoolPriceRow `json:"prices"`
}

type workerPoolPriceRow struct {
	SKU           string  `json:"sku,omitempty"`
	MachineType   string  `json:"machine_type,omitempty"`
	Region        string  `json:"region"`
	Configured    bool    `json:"configured"`
	SpotPrice     float64 `json:"spot_price"`
	OnDemandPrice float64 `json:"on_demand_price,omitempty"`
	Commit1Yr     float64 `json:"commit_1yr_price,omitempty"`
	Commit3Yr     float64 `json:"commit_3yr_price,omitempty"`
	Currency      string  `json:"currency,omitempty"`
	Unit          string  `json:"unit,omitempty"`
	MeterName     string  `json:"meter_name,omitempty"`
	ProductName   string  `json:"product_name,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	OS            string  `json:"os,omitempty"`
}

type workerPoolInput struct {
	Reader io.Reader
	Source string
	Close  func() error
}

type priceKey struct {
	resource string
	region   string
}

func init() {
	workerPoolCmd.Flags().StringVar(&flagWorkerPoolFrom, "from", "", "tc-admin JSON file to read instead of stdin; use - for stdin")
	workerPoolCmd.Flags().StringVar(&flagWorkerPoolOS, "os", "any", "Azure OS filter: linux | windows | any")
	workerPoolCmd.Flags().BoolVar(&flagWorkerPoolConfiguredOnly, "configured-only", false,
		"only print regions configured on the worker pool")
	workerPoolCmd.Flags().StringVar(&flagWorkerPoolAPIKey, "api-key", "", "cloudprice.net subscription key for GCP worker pools")
	rootCmd.AddCommand(workerPoolCmd)
}

func runWorkerPool(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateOSFilter(flagWorkerPoolOS); err != nil {
		return err
	}
	osFilter := strings.ToLower(strings.TrimSpace(flagWorkerPoolOS))
	if osFilter == "" {
		osFilter = "any"
	}

	workerPoolID := ""
	if len(args) == 1 {
		workerPoolID = args[0]
	}

	input, err := openWorkerPoolInput(flagWorkerPoolFrom)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()

	target, err := tcadmin.DecodeWorkerPoolTarget(input.Reader, workerPoolID)
	if err != nil {
		return err
	}

	var report workerPoolPriceReport
	switch target.Cloud {
	case tcadmin.CloudAzure:
		report, err = buildAzureWorkerPoolReport(ctx, target, input.Source, osFilter, flagWorkerPoolConfiguredOnly)
	case tcadmin.CloudGCP:
		report, err = buildGCPWorkerPoolReport(ctx, target, input.Source, flagWorkerPoolAPIKey, flagWorkerPoolConfiguredOnly)
	default:
		err = fmt.Errorf("unsupported cloud %q", target.Cloud)
	}
	if err != nil {
		return err
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	renderWorkerPoolReport(report)
	return nil
}

func openWorkerPoolInput(path string) (workerPoolInput, error) {
	switch path {
	case "":
		info, err := os.Stdin.Stat()
		if err != nil {
			return workerPoolInput{}, fmt.Errorf("stat stdin: %w", err)
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return workerPoolInput{}, usageErrorf("pipe tc-admin JSON on stdin or pass --from <path>")
		}
		return workerPoolInput{
			Reader: os.Stdin,
			Source: "stdin",
			Close:  func() error { return nil },
		}, nil
	case "-":
		return workerPoolInput{
			Reader: os.Stdin,
			Source: "stdin",
			Close:  func() error { return nil },
		}, nil
	default:
		f, err := os.Open(path)
		if err != nil {
			return workerPoolInput{}, fmt.Errorf("open %s: %w", path, err)
		}
		return workerPoolInput{
			Reader: f,
			Source: path,
			Close:  f.Close,
		}, nil
	}
}

func buildAzureWorkerPoolReport(ctx context.Context, target tcadmin.WorkerPoolTarget, source, osFilter string, configuredOnly bool) (workerPoolPriceReport, error) {
	report := baseWorkerPoolReport(target, source, osFilter)
	configured := configuredSet(target, true)
	client := newAzureRetailSpotClient()

	for _, sku := range target.AzureSKUs {
		progress("fetching Azure Retail Prices for %s ...", sku)
		items, err := client.SpotPrices(ctx, sku)
		if err != nil {
			return workerPoolPriceReport{}, err
		}
		for _, item := range filterAzureRetail(items, "", osFilter) {
			rowSKU := item.ArmSkuName
			if rowSKU == "" {
				rowSKU = sku
			}
			rowConfigured := configured[priceKey{resource: sku, region: item.ArmRegionName}] ||
				configured[priceKey{resource: rowSKU, region: item.ArmRegionName}]
			if configuredOnly && !rowConfigured {
				continue
			}
			report.Prices = append(report.Prices, workerPoolPriceRow{
				SKU:         rowSKU,
				Region:      item.ArmRegionName,
				Configured:  rowConfigured,
				SpotPrice:   item.RetailPrice,
				Currency:    item.CurrencyCode,
				Unit:        item.UnitOfMeasure,
				MeterName:   item.MeterName,
				ProductName: item.ProductName,
				OS:          azureProductOS(item.ProductName),
			})
		}
	}
	sortWorkerPoolPriceRows(report.Prices)
	return report, nil
}

func buildGCPWorkerPoolReport(ctx context.Context, target tcadmin.WorkerPoolTarget, source, apiKey string, configuredOnly bool) (workerPoolPriceReport, error) {
	key, err := requireCloudpriceKey(apiKey)
	if err != nil {
		return workerPoolPriceReport{}, err
	}

	report := baseWorkerPoolReport(target, source, "")
	configured := configuredSet(target, false)
	client := newWorkerPoolGCPClient(key)

	for _, machineType := range target.GCPMachineTypes {
		progress("fetching GCP current prices for %s ...", machineType)
		resp, err := client.GCPCurrent(ctx, machineType)
		if err != nil {
			return workerPoolPriceReport{}, err
		}
		for _, price := range resp.Data.Prices {
			rowConfigured := configured[priceKey{resource: machineType, region: price.Region}]
			if configuredOnly && !rowConfigured {
				continue
			}
			report.Prices = append(report.Prices, workerPoolPriceRow{
				MachineType:   machineType,
				Region:        price.Region,
				Configured:    rowConfigured,
				SpotPrice:     price.PriceSpot,
				OnDemandPrice: price.PriceOnDemand,
				Commit1Yr:     price.PriceCommit1Yr,
				Commit3Yr:     price.PriceCommit3Yr,
				Currency:      resp.Data.Currency,
				UpdatedAt:     resp.Data.UpdatedAt,
				OS:            resp.Data.OSandSoftware,
			})
		}
	}
	sortWorkerPoolPriceRows(report.Prices)
	return report, nil
}

func baseWorkerPoolReport(target tcadmin.WorkerPoolTarget, source, osFilter string) workerPoolPriceReport {
	return workerPoolPriceReport{
		WorkerPoolID:      target.ID,
		Source:            source,
		Cloud:             string(target.Cloud),
		ProviderID:        target.ProviderID,
		OSFilter:          osFilter,
		ConfiguredRegions: target.Regions,
		SKUs:              target.AzureSKUs,
		MachineTypes:      target.GCPMachineTypes,
		Prices:            []workerPoolPriceRow{},
	}
}

func configuredSet(target tcadmin.WorkerPoolTarget, azure bool) map[priceKey]bool {
	out := make(map[priceKey]bool)
	for _, launch := range target.Launches {
		resource := launch.MachineType
		if azure {
			resource = launch.AzureSKU
		}
		out[priceKey{resource: resource, region: launch.Region}] = true
	}
	return out
}

func azureProductOS(productName string) string {
	if strings.Contains(strings.ToLower(productName), "windows") {
		return "windows"
	}
	return ""
}

func sortWorkerPoolPriceRows(rows []workerPoolPriceRow) {
	sort.Slice(rows, func(i, j int) bool {
		ri := rows[i].SKU
		rj := rows[j].SKU
		if ri == "" {
			ri = rows[i].MachineType
		}
		if rj == "" {
			rj = rows[j].MachineType
		}
		if ri != rj {
			return ri < rj
		}
		if rows[i].SpotPrice != rows[j].SpotPrice {
			return rows[i].SpotPrice < rows[j].SpotPrice
		}
		if rows[i].Region != rows[j].Region {
			return rows[i].Region < rows[j].Region
		}
		return rows[i].ProductName < rows[j].ProductName
	})
}

func renderWorkerPoolReport(report workerPoolPriceReport) {
	fmt.Printf("Worker pool: %s\n", report.WorkerPoolID)
	fmt.Printf("Cloud:       %s (%s)\n", report.Cloud, report.ProviderID)
	if report.Source != "" {
		fmt.Printf("Source:      %s\n", report.Source)
	}
	if len(report.SKUs) > 0 {
		fmt.Printf("SKUs:        %s\n", strings.Join(report.SKUs, ", "))
	}
	if len(report.MachineTypes) > 0 {
		fmt.Printf("Machines:    %s\n", strings.Join(report.MachineTypes, ", "))
	}
	fmt.Printf("Configured:  %s\n\n", strings.Join(report.ConfiguredRegions, ", "))

	if len(report.Prices) == 0 {
		fmt.Fprintln(os.Stderr, "no price rows returned")
		return
	}

	if report.Cloud == string(tcadmin.CloudGCP) {
		renderGCPWorkerPoolRows(report.Prices)
		return
	}
	renderAzureWorkerPoolRows(report.Prices)
}

func renderAzureWorkerPoolRows(rows []workerPoolPriceRow) {
	fmt.Printf("%-24s %-18s %-10s %-10s %-8s %-12s  %s\n",
		"sku", "region", "configured", "spot", "currency", "unit", "meter / product")
	for _, row := range rows {
		fmt.Printf("%-24s %-18s %-10s %-10.4f %-8s %-12s  %s - %s\n",
			row.SKU, row.Region, yesNo(row.Configured), row.SpotPrice, row.Currency, row.Unit,
			row.MeterName, row.ProductName)
	}
}

func renderGCPWorkerPoolRows(rows []workerPoolPriceRow) {
	fmt.Printf("%-24s %-18s %-10s %-10s %-10s %-10s %-10s %-8s\n",
		"machine", "region", "configured", "spot", "on-demand", "1yr", "3yr", "currency")
	for _, row := range rows {
		fmt.Printf("%-24s %-18s %-10s %-10.4f %-10.4f %-10.4f %-10.4f %-8s\n",
			row.MachineType, row.Region, yesNo(row.Configured), row.SpotPrice,
			row.OnDemandPrice, row.Commit1Yr, row.Commit3Yr, row.Currency)
	}
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
