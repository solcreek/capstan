package capstan

import (
	"encoding/json"
	"fmt"
)

type Geography string

const (
	GeoEUCentral  Geography = "eu-central"
	GeoEUNorth    Geography = "eu-north"
	GeoEUWest     Geography = "eu-west"
	GeoEUSouth    Geography = "eu-south"
	GeoUSEast     Geography = "us-east"
	GeoUSCentral  Geography = "us-central"
	GeoUSWest     Geography = "us-west"
	GeoCanada     Geography = "canada"
	GeoSingapore  Geography = "singapore"
	GeoJapan      Geography = "japan"
	GeoIndia      Geography = "india"
	GeoIndonesia  Geography = "indonesia"
	GeoAustralia  Geography = "australia"
	GeoLatam      Geography = "latam"
)

type Workload string

const (
	WorkloadIOMultitenant Workload = "io-multitenant"
	WorkloadCPULatency    Workload = "cpu-latency"
	WorkloadCPUThroughput Workload = "cpu-throughput"
	WorkloadGeneral       Workload = "general"
)

type SlaTier string

const (
	SLABestEffort SlaTier = "best-effort"
	SLAStandard   SlaTier = "standard"
	SLAPremium    SlaTier = "premium"
)

type Placement struct {
	Provider ProviderName `json:"provider"`
	Region   string       `json:"region"`
	Size     string       `json:"size"`
}

type Recommendation struct {
	Primary   Placement   `json:"primary"`
	Fallbacks []Placement `json:"fallbacks"`
	Caveats   []string    `json:"caveats"`
}

type postureData struct {
	GeoRouting  map[Geography][]regionCandidate    `json:"geoRouting"`
	TierMap     map[ProviderName]map[Workload]map[SlaTier]string `json:"tierMap"`
	CellCaveats map[string][]string                `json:"cellCaveats"`
	SizeCaveats map[string][]string                `json:"sizeCaveats"`
}

type regionCandidate struct {
	Provider ProviderName `json:"provider"`
	Region   string       `json:"region"`
}

var posture postureData

func init() {
	if err := json.Unmarshal(postureSpecData, &posture); err != nil {
		panic("capstan: bad embedded posture spec: " + err.Error())
	}
}

func RecommendPlacement(geography Geography, workload Workload, sla SlaTier) (*Recommendation, error) {
	if workload == "" {
		workload = WorkloadIOMultitenant
	}
	if sla == "" {
		sla = SLAStandard
	}

	candidates, ok := posture.GeoRouting[geography]
	if !ok || len(candidates) == 0 {
		return nil, fmt.Errorf("capstan: no regions catalogued for geography %q", geography)
	}

	placements := make([]Placement, 0, len(candidates))
	for _, c := range candidates {
		tierMap, ok := posture.TierMap[c.Provider]
		if !ok {
			continue
		}
		workloadMap, ok := tierMap[workload]
		if !ok {
			continue
		}
		size, ok := workloadMap[sla]
		if !ok {
			continue
		}
		placements = append(placements, Placement{
			Provider: c.Provider,
			Region:   c.Region,
			Size:     size,
		})
	}

	if len(placements) == 0 {
		return nil, fmt.Errorf("capstan: no placements for geography %q, workload %q, sla %q", geography, workload, sla)
	}

	primary := placements[0]
	var caveats []string
	cellKey := string(primary.Provider) + "/" + primary.Region
	sizeKey := string(primary.Provider) + "/" + primary.Size
	if cc, ok := posture.CellCaveats[cellKey]; ok {
		caveats = append(caveats, cc...)
	}
	if sc, ok := posture.SizeCaveats[sizeKey]; ok {
		caveats = append(caveats, sc...)
	}

	return &Recommendation{
		Primary:   primary,
		Fallbacks: placements[1:],
		Caveats:   caveats,
	}, nil
}
