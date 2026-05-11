package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cfg "github.com/jwmossmoz/costctl/internal/config"
)

// shared gcp flags
var (
	flagMachine   string
	flagGCPRegion string
	flagDays      int
	flagGCPAPIKey string
)

var gcpCmd = &cobra.Command{
	Use:   "gcp",
	Short: "GCP pricing queries",
}

var gcpSpotCmd = &cobra.Command{
	Use:   "spot",
	Short: "GCP Compute Engine spot/preemptible pricing (current + historical)",
}

var gcpSpotCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Current GCP spot prices across all regions (via cloudprice.net)",
	Example: strings.TrimSpace(`
  costctl gcp spot current --machine-type n2-standard-2
  costctl gcp spot current --machine-type c2-standard-16 --region us-central1
  costctl gcp spot current --machine-type n2-standard-2 --json`),
	RunE: runGCPSpotCurrent,
}

var gcpSpotHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "~90 days of GCP spot price snapshots (via cloudprice.net)",
	Example: strings.TrimSpace(`
  costctl gcp spot history --machine-type n2-standard-2 --region us-central1
  costctl gcp spot history --machine-type c2-standard-16 --region us-east1 --days 30
  costctl gcp spot history --machine-type n2-standard-2 --region us-central1 --json`),
	RunE: runGCPSpotHistory,
}

func init() {
	for _, c := range []*cobra.Command{gcpSpotCurrentCmd, gcpSpotHistoryCmd} {
		c.Flags().StringVar(&flagMachine, "machine-type", "", "GCP machine type, e.g. n2-standard-2 (required)")
		c.Flags().StringVar(&flagGCPRegion, "region", "", "GCP region (e.g. us-central1); current: filter; history: required")
		c.Flags().StringVar(&flagGCPAPIKey, "api-key", "", "cloudprice.net subscription key (overrides env/config)")
		_ = c.MarkFlagRequired("machine-type")
	}
	gcpSpotHistoryCmd.Flags().IntVar(&flagDays, "days", 90, "history window in days (max ~90 per upstream)")

	gcpSpotCmd.AddCommand(gcpSpotCurrentCmd, gcpSpotHistoryCmd)
	gcpCmd.AddCommand(gcpSpotCmd)
	rootCmd.AddCommand(gcpCmd)
}

func requireCloudpriceKey(flagVal string) (string, error) {
	key, source, err := cfg.ResolveAPIKey(cfg.ProviderCloudprice, flagVal, envCloudpriceKey)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", fmt.Errorf("no cloudprice.net key found. Pass --api-key, set %s, "+
			"or run: costctl config set-key cloudprice <KEY>", envCloudpriceKey)
	}
	if flagVerbose {
		fmt.Fprintf(os.Stderr, "using cloudprice key from %s\n", source)
	}
	return key, nil
}

func runGCPSpotCurrent(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateGCPSpotCurrentFlags(); err != nil {
		return err
	}
	key, err := requireCloudpriceKey(flagGCPAPIKey)
	if err != nil {
		return err
	}

	progress("fetching GCP current prices for %s ...", flagMachine)
	resp, err := newCloudpriceClient(key).GCPCurrent(ctx, flagMachine)
	if err != nil {
		return err
	}

	prices := resp.Data.Prices
	if flagGCPRegion != "" {
		filtered := prices[:0]
		for _, p := range prices {
			if p.Region == flagGCPRegion {
				filtered = append(filtered, p)
			}
		}
		prices = filtered
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(prices)
	}
	if len(prices) == 0 {
		fmt.Fprintf(os.Stderr, "no rows match (machine-type=%s region=%s)\n",
			flagMachine, flagGCPRegion)
		return nil
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i].Region < prices[j].Region })
	fmt.Printf("Machine type: %s   OS: %s   Updated: %s   Currency: %s\n\n",
		flagMachine, resp.Data.OSandSoftware, resp.Data.UpdatedAt, resp.Data.Currency)
	fmt.Printf("%-22s %-12s %-12s %-12s %-12s\n", "region", "spot", "on-demand", "1yr-commit", "3yr-commit")
	for _, p := range prices {
		fmt.Printf("%-22s %-12.4f %-12.4f %-12.4f %-12.4f\n",
			p.Region, p.PriceSpot, p.PriceOnDemand, p.PriceCommit1Yr, p.PriceCommit3Yr)
	}
	return nil
}

func runGCPSpotHistory(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateGCPSpotHistoryFlags(); err != nil {
		return err
	}
	key, err := requireCloudpriceKey(flagGCPAPIKey)
	if err != nil {
		return err
	}

	startDate := time.Now().AddDate(0, 0, -flagDays).Format("20060102")

	progress("fetching GCP history for %s in %s since %s ...",
		flagMachine, flagGCPRegion, startDate)
	resp, err := newCloudpriceClient(key).GCPHistory(ctx, flagMachine, flagGCPRegion, startDate)
	if err != nil {
		return err
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(resp)
	}
	items := resp.Data.Items
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "no history rows returned")
		return nil
	}
	// Sort oldest -> newest for readability.
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedYYYYMMDD < items[j].CreatedYYYYMMDD })

	fmt.Printf("Machine type: %s   Region: %s   OS: %s   Currency: %s\n",
		flagMachine, resp.Data.Region, resp.Data.OSandSoftware, resp.Data.Currency)
	fmt.Printf("Window:       %s -> %s   (%d snapshots)\n\n",
		resp.Data.StartDate, resp.Data.EndDate, len(items))
	fmt.Printf("%-22s %-12s %-12s %-12s %-12s\n", "createdDateTime", "spot", "on-demand", "1yr-commit", "3yr-commit")
	for _, it := range items {
		fmt.Printf("%-22s %-12.4f %-12.4f %-12.4f %-12.4f\n",
			it.CreatedDateTime, it.PriceSpot, it.PriceOnDemand, it.PriceCommit1Yr, it.PriceCommit3Yr)
	}
	return nil
}

func validateGCPSpotCurrentFlags() error {
	flagMachine = strings.TrimSpace(flagMachine)
	flagGCPRegion = strings.TrimSpace(flagGCPRegion)
	if flagMachine == "" {
		return usageErrorf("--machine-type is required")
	}
	return nil
}

func validateGCPSpotHistoryFlags() error {
	if err := validateGCPSpotCurrentFlags(); err != nil {
		return err
	}
	if strings.TrimSpace(flagGCPRegion) == "" {
		return usageErrorf("--region is required for history")
	}
	if flagDays < 1 || flagDays > 90 {
		return usageErrorf("--days must be between 1 and 90")
	}
	return nil
}
