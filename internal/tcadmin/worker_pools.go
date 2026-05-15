// Package tcadmin extracts pricing inputs from tc-admin generated resources.
package tcadmin

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// Cloud identifies the worker-manager provider family for a worker pool.
type Cloud string

const (
	// CloudAzure is Taskcluster's Azure provider.
	CloudAzure Cloud = "azure"
	// CloudGCP is Taskcluster's GCP provider.
	CloudGCP Cloud = "gcp"
)

// WorkerPoolTarget is the price-queryable subset of one tc-admin WorkerPool.
type WorkerPoolTarget struct {
	ID              string
	ProviderID      string
	Cloud           Cloud
	Launches        []LaunchConfig
	AzureSKUs       []string
	GCPMachineTypes []string
	Regions         []string
}

// LaunchConfig is one configured place where a worker pool can run.
type LaunchConfig struct {
	Region      string
	AzureSKU    string
	MachineType string
}

type document struct {
	Resources []resource `json:"resources"`
}

type resource struct {
	Kind         string `json:"kind"`
	WorkerPoolID string `json:"workerPoolId"`
	ProviderID   string `json:"providerId"`
	Config       struct {
		LaunchConfigs []launchConfig `json:"launchConfigs"`
	} `json:"config"`
}

type launchConfig struct {
	Location      string `json:"location"`
	Region        string `json:"region"`
	Zone          string `json:"zone"`
	MachineType   string `json:"machineType"`
	ArmDeployment struct {
		Parameters map[string]parameter `json:"parameters"`
	} `json:"armDeployment"`
	HardwareProfile struct {
		VMSize string `json:"vmSize"`
	} `json:"hardwareProfile"`
}

type parameter struct {
	Value any `json:"value"`
}

// DecodeWorkerPoolTarget reads tc-admin --json output and returns one worker
// pool's cloud, regions, and SKU/machine-type inputs for pricing queries.
func DecodeWorkerPoolTarget(r io.Reader, workerPoolID string) (WorkerPoolTarget, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return WorkerPoolTarget{}, fmt.Errorf("read tc-admin JSON: %w", err)
	}

	resources, err := decodeResources(body)
	if err != nil {
		return WorkerPoolTarget{}, err
	}
	if len(resources) == 0 {
		return WorkerPoolTarget{}, fmt.Errorf("tc-admin JSON contains no resources")
	}

	var matches []resource
	for _, res := range resources {
		if res.Kind != "" && res.Kind != "WorkerPool" {
			continue
		}
		if workerPoolID == "" || res.WorkerPoolID == workerPoolID {
			matches = append(matches, res)
		}
	}
	if len(matches) == 0 {
		if workerPoolID == "" {
			return WorkerPoolTarget{}, fmt.Errorf("tc-admin JSON contains no WorkerPool resources")
		}
		return WorkerPoolTarget{}, fmt.Errorf("worker pool %q not found in tc-admin JSON", workerPoolID)
	}
	if workerPoolID == "" && len(matches) > 1 {
		return WorkerPoolTarget{}, fmt.Errorf("worker pool id is required when input contains %d WorkerPool resources", len(matches))
	}
	if len(matches) > 1 {
		return WorkerPoolTarget{}, fmt.Errorf("worker pool %q appears %d times in tc-admin JSON", workerPoolID, len(matches))
	}

	return targetFromResource(matches[0])
}

func decodeResources(body []byte) ([]resource, error) {
	var doc document
	if err := json.Unmarshal(body, &doc); err == nil && doc.Resources != nil {
		return doc.Resources, nil
	}

	var resources []resource
	if err := json.Unmarshal(body, &resources); err == nil {
		return resources, nil
	}

	return nil, fmt.Errorf("decode tc-admin JSON: expected an object with resources or an array of resources")
}

func targetFromResource(res resource) (WorkerPoolTarget, error) {
	cloud, err := cloudFromProvider(res.ProviderID)
	if err != nil {
		return WorkerPoolTarget{}, err
	}

	target := WorkerPoolTarget{
		ID:         res.WorkerPoolID,
		ProviderID: res.ProviderID,
		Cloud:      cloud,
	}
	for _, lc := range res.Config.LaunchConfigs {
		launch := LaunchConfig{
			Region:      regionFromLaunch(lc, cloud),
			AzureSKU:    strings.TrimSpace(azureSKUFromLaunch(lc)),
			MachineType: strings.TrimSpace(machineTypeFromLaunch(lc)),
		}
		switch cloud {
		case CloudAzure:
			if launch.Region == "" || launch.AzureSKU == "" {
				continue
			}
		case CloudGCP:
			if launch.Region == "" || launch.MachineType == "" {
				continue
			}
		}
		target.Launches = append(target.Launches, launch)
	}
	if len(target.Launches) == 0 {
		return WorkerPoolTarget{}, fmt.Errorf("worker pool %q has no pricing-compatible launch configs", res.WorkerPoolID)
	}

	target.Regions = uniqueSortedFunc(target.Launches, func(l LaunchConfig) string { return l.Region })
	target.AzureSKUs = uniqueSortedFunc(target.Launches, func(l LaunchConfig) string { return l.AzureSKU })
	target.GCPMachineTypes = uniqueSortedFunc(target.Launches, func(l LaunchConfig) string { return l.MachineType })
	return target, nil
}

func cloudFromProvider(providerID string) (Cloud, error) {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	switch {
	case strings.Contains(providerID, "azure"):
		return CloudAzure, nil
	case strings.Contains(providerID, "gcp"):
		return CloudGCP, nil
	default:
		return "", fmt.Errorf("unsupported provider %q (want Azure or GCP)", providerID)
	}
}

func regionFromLaunch(lc launchConfig, cloud Cloud) string {
	if lc.Region != "" {
		return strings.TrimSpace(lc.Region)
	}
	if lc.Location != "" {
		return strings.TrimSpace(lc.Location)
	}
	if v := parameterString(lc.ArmDeployment.Parameters, "location"); v != "" {
		return v
	}
	if cloud == CloudGCP {
		return regionFromZone(lc.Zone)
	}
	return ""
}

func azureSKUFromLaunch(lc launchConfig) string {
	if v := parameterString(lc.ArmDeployment.Parameters, "vmSize"); v != "" {
		return v
	}
	return lc.HardwareProfile.VMSize
}

func machineTypeFromLaunch(lc launchConfig) string {
	if lc.MachineType == "" {
		return ""
	}
	return path.Base(lc.MachineType)
}

func parameterString(params map[string]parameter, key string) string {
	if params == nil {
		return ""
	}
	p, ok := params[key]
	if !ok {
		return ""
	}
	s, ok := p.Value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func regionFromZone(zone string) string {
	zone = strings.TrimSpace(zone)
	parts := strings.Split(zone, "-")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "-")
}

func uniqueSortedFunc(launches []LaunchConfig, value func(LaunchConfig) string) []string {
	seen := make(map[string]struct{})
	for _, l := range launches {
		v := value(l)
		if v == "" {
			continue
		}
		seen[v] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
