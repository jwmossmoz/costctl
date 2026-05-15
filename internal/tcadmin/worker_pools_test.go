package tcadmin

import (
	"strings"
	"testing"
)

func TestDecodeWorkerPoolTargetAzure(t *testing.T) {
	const body = `{
		"resources": [{
			"kind": "WorkerPool",
			"workerPoolId": "gecko-t/win11-64-25h2-amd",
			"providerId": "azure2",
			"config": {
				"launchConfigs": [
					{"location":"centralindia","armDeployment":{"parameters":{"vmSize":{"value":"Standard_E8ads_v6"}}}},
					{"armDeployment":{"parameters":{"location":{"value":"uksouth"},"vmSize":{"value":"Standard_E8ads_v6"}}}}
				]
			}
		}]
	}`

	got, err := DecodeWorkerPoolTarget(strings.NewReader(body), "")
	if err != nil {
		t.Fatalf("DecodeWorkerPoolTarget: %v", err)
	}
	if got.ID != "gecko-t/win11-64-25h2-amd" || got.Cloud != CloudAzure {
		t.Fatalf("target identity = %+v", got)
	}
	if len(got.AzureSKUs) != 1 || got.AzureSKUs[0] != "Standard_E8ads_v6" {
		t.Fatalf("AzureSKUs = %#v", got.AzureSKUs)
	}
	wantRegions := []string{"centralindia", "uksouth"}
	if strings.Join(got.Regions, ",") != strings.Join(wantRegions, ",") {
		t.Fatalf("Regions = %#v, want %#v", got.Regions, wantRegions)
	}
}

func TestDecodeWorkerPoolTargetGCP(t *testing.T) {
	const body = `[{
		"kind": "WorkerPool",
		"workerPoolId": "gecko-t/t-linux-docker-amd",
		"providerId": "fxci-level1-gcp",
		"config": {
			"launchConfigs": [
				{"machineType":"zones/us-central1-a/machineTypes/c3d-standard-8-lssd","zone":"us-central1-a"},
				{"machineType":"zones/us-east1-b/machineTypes/c3d-standard-8-lssd","region":"us-east1"}
			]
		}
	}]`

	got, err := DecodeWorkerPoolTarget(strings.NewReader(body), "gecko-t/t-linux-docker-amd")
	if err != nil {
		t.Fatalf("DecodeWorkerPoolTarget: %v", err)
	}
	if got.Cloud != CloudGCP {
		t.Fatalf("Cloud = %q, want %q", got.Cloud, CloudGCP)
	}
	if len(got.GCPMachineTypes) != 1 || got.GCPMachineTypes[0] != "c3d-standard-8-lssd" {
		t.Fatalf("GCPMachineTypes = %#v", got.GCPMachineTypes)
	}
	if strings.Join(got.Regions, ",") != "us-central1,us-east1" {
		t.Fatalf("Regions = %#v", got.Regions)
	}
}

func TestDecodeWorkerPoolTargetRequiresIDForMultiplePools(t *testing.T) {
	const body = `{"resources":[
		{"kind":"WorkerPool","workerPoolId":"a","providerId":"azure2","config":{"launchConfigs":[]}},
		{"kind":"WorkerPool","workerPoolId":"b","providerId":"azure2","config":{"launchConfigs":[]}}
	]}`

	_, err := DecodeWorkerPoolTarget(strings.NewReader(body), "")
	if err == nil || !strings.Contains(err.Error(), "worker pool id is required") {
		t.Fatalf("expected multiple-pool error, got %v", err)
	}
}
