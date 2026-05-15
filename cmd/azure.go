package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jwmossmoz/costctl/internal/azureretail"
)

const envCloudpriceKey = "CLOUDPRICE_API_KEY"

// shared azure spot flags
var (
	flagAPIKey string
	flagSKU    string
	flagRegion string
	flagOS     string
	flagTier   string
)

var azureCmd = &cobra.Command{
	Use:   "azure",
	Short: "Azure pricing queries",
}

var azureSpotCmd = &cobra.Command{
	Use:   "spot",
	Short: "Azure VM spot pricing (current + historical)",
}

var azureSpotCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Current spot prices from the Azure Retail Prices API (no key required)",
	Example: strings.TrimSpace(`
  costctl azure spot current --sku Standard_F8s_v2
  costctl azure spot current --sku Standard_F8s_v2 --region westus2 --os linux
  costctl azure spot current --sku Standard_F8s_v2 --json`),
	RunE: runAzureSpotCurrent,
}

var azureSpotHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "~90 days of spot price changes from cloudprice.net (requires key)",
	Example: strings.TrimSpace(`
  costctl azure spot history --sku Standard_F8s_v2 --region westus2
  costctl azure spot history --sku Standard_F8s_v2 --region eastus2 --os windows
  costctl azure spot history --sku Standard_F8s_v2 --region westus2 --json`),
	RunE: runAzureSpotHistory,
}

func init() {
	for _, c := range []*cobra.Command{azureSpotCurrentCmd, azureSpotHistoryCmd} {
		c.Flags().StringVar(&flagSKU, "sku", "", "Azure VM SKU, e.g. Standard_F8s_v2 (required)")
		c.Flags().StringVar(&flagRegion, "region", "", "Azure region (e.g. westus2); omit on current to list all")
		c.Flags().StringVar(&flagOS, "os", "any", "filter rows: linux | windows | any")
		_ = c.MarkFlagRequired("sku")
	}
	azureSpotHistoryCmd.Flags().StringVar(&flagAPIKey, "api-key", "", "cloudprice.net subscription key (overrides env/config)")
	azureSpotHistoryCmd.Flags().StringVar(&flagTier, "tier", "spot", "price tier: spot | standard | low")

	azureSpotCmd.AddCommand(azureSpotCurrentCmd, azureSpotHistoryCmd)
	azureCmd.AddCommand(azureSpotCmd)
	rootCmd.AddCommand(azureCmd)
}

func runAzureSpotCurrent(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateAzureSpotCurrentFlags(); err != nil {
		return err
	}
	progress("fetching Azure Retail Prices for %s ...", flagSKU)
	items, err := azureretail.New().SpotPrices(ctx, flagSKU)
	if err != nil {
		return err
	}

	rows := filterAzureRetail(items, flagRegion, flagOS)
	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintf(os.Stderr, "no rows match (sku=%s region=%s os=%s)\n",
			flagSKU, flagRegion, flagOS)
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ArmRegionName != rows[j].ArmRegionName {
			return rows[i].ArmRegionName < rows[j].ArmRegionName
		}
		return rows[i].ProductName < rows[j].ProductName
	})
	fmt.Printf("%-18s %-9s %-10s %-12s  %s\n", "region", "price", "currency", "unit", "meter / product")
	for _, r := range rows {
		fmt.Printf("%-18s %-9.4f %-10s %-12s  %s — %s\n",
			r.ArmRegionName, r.RetailPrice, r.CurrencyCode, r.UnitOfMeasure,
			r.MeterName, r.ProductName)
	}
	return nil
}

func runAzureSpotHistory(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateAzureSpotHistoryFlags(); err != nil {
		return err
	}

	key, err := requireCloudpriceKey(flagAPIKey)
	if err != nil {
		return err
	}

	progress("fetching cloudprice history for %s in %s ...", flagSKU, regionLabel(flagRegion))
	client := newCloudpriceClient(key)
	resp, err := client.PriceHistory(ctx, flagSKU, flagRegion, flagTier)
	if err != nil {
		return err
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Fprintln(os.Stderr, "no history rows returned")
		return nil
	}
	fmt.Printf("SKU:       %s\n", flagSKU)
	fmt.Printf("Region:    %v\n", resp.Regions)
	fmt.Printf("Tier:      %s\n", resp.Tier)
	fmt.Printf("Currency:  %s\n", resp.Currency)
	fmt.Printf("Window:    %s -> %s  (%d change points)\n",
		resp.FromDate, resp.ToDate, len(resp.Items))
	fmt.Printf("Last seen: %s\n\n", resp.LastUpdate)

	rows := resp.Items
	// API returns newest first; render oldest-first for readability.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	showLinux := flagOS != "windows"
	showWin := flagOS != "linux"
	fmt.Printf("%-22s", "modifiedDate")
	if showLinux {
		fmt.Printf(" %-12s", "linuxPrice")
	}
	if showWin {
		fmt.Printf(" %-12s", "windowsPrice")
	}
	fmt.Println()
	for _, it := range rows {
		fmt.Printf("%-22s", it.ModifiedDate)
		if showLinux {
			fmt.Printf(" %-12.4f", it.LinuxPrice)
		}
		if showWin {
			fmt.Printf(" %-12.4f", it.WindowsPrice)
		}
		fmt.Println()
	}
	return nil
}

func filterAzureRetail(items []azureretail.Item, region, osFilter string) []azureretail.Item {
	out := make([]azureretail.Item, 0, len(items))
	for _, it := range items {
		if region != "" && it.ArmRegionName != region {
			continue
		}
		if !matchOS(it.ProductName, osFilter) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func matchOS(productName, osFilter string) bool {
	isWindows := strings.Contains(strings.ToLower(productName), "windows")
	switch strings.ToLower(strings.TrimSpace(osFilter)) {
	case "", "any":
		return true
	case "windows":
		return isWindows
	case "linux":
		return !isWindows
	}
	return true
}

func validateAzureSpotCurrentFlags() error {
	flagSKU = strings.TrimSpace(flagSKU)
	flagRegion = strings.TrimSpace(flagRegion)
	if strings.TrimSpace(flagSKU) == "" {
		return usageErrorf("--sku is required")
	}
	if err := validateOSFilter(flagOS); err != nil {
		return err
	}
	flagOS = strings.ToLower(strings.TrimSpace(flagOS))
	return nil
}

func validateAzureSpotHistoryFlags() error {
	if err := validateAzureSpotCurrentFlags(); err != nil {
		return err
	}
	flagTier = strings.ToLower(strings.TrimSpace(flagTier))
	switch flagTier {
	case "spot", "standard", "low":
		return nil
	default:
		return usageErrorf("invalid --tier %q (want spot, standard, or low)", flagTier)
	}
}

func validateOSFilter(osFilter string) error {
	switch strings.ToLower(strings.TrimSpace(osFilter)) {
	case "", "any", "linux", "windows":
		return nil
	default:
		return usageErrorf("invalid --os %q (want linux, windows, or any)", osFilter)
	}
}

func regionLabel(r string) string {
	if r == "" {
		return "(API default)"
	}
	return r
}

func progress(format string, args ...any) {
	if flagQuiet {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
