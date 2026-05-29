package capstan

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
)

type VultrProvider struct {
	spec *ProviderSpec
	http *httpClient
}

func NewVultr(token string) *VultrProvider {
	return &VultrProvider{
		spec: Spec(Vultr),
		http: newHTTPClient("vultr", token),
	}
}

func (v *VultrProvider) Name() ProviderName { return Vultr }

func (v *VultrProvider) Regions(ctx context.Context) ([]Region, error) {
	var resp struct {
		Regions []struct {
			ID      string `json:"id"`
			City    string `json:"city"`
			Country string `json:"country"`
		} `json:"regions"`
	}
	if err := v.http.GET(ctx, v.spec.BaseURL,"/regions?per_page=500", &resp); err != nil {
		return nil, err
	}
	regions := make([]Region, len(resp.Regions))
	for i, r := range resp.Regions {
		regions[i] = Region{
			ID:      r.ID,
			Name:    r.City,
			Country: r.Country,
			City:    r.City,
		}
	}
	return regions, nil
}

func (v *VultrProvider) Plans(ctx context.Context, region string) ([]Plan, error) {
	var resp struct {
		Plans []struct {
			ID       string `json:"id"`
			VCPUCount int   `json:"vcpu_count"`
			RAM      int    `json:"ram"`
			Disk     int    `json:"disk"`
		} `json:"plans"`
	}
	if err := v.http.GET(ctx, v.spec.BaseURL,"/plans?per_page=500", &resp); err != nil {
		return nil, err
	}
	plans := make([]Plan, len(resp.Plans))
	for i, p := range resp.Plans {
		plans[i] = Plan{
			ID:            p.ID,
			Name:          p.ID,
			CPUs:          p.VCPUCount,
			MemoryMB:      p.RAM,
			DiskGB:        p.Disk,
			MonthlyCents:  v.spec.EstimateMonthlyCost(p.ID),
			PriceCurrency: v.spec.PriceCurrency,
		}
	}
	return plans, nil
}

func (v *VultrProvider) Create(ctx context.Context, opts CreateOpts) (*Server, error) {
	osID, err := resolveVultrOSID(v.spec.ResolveImage(opts.Image))
	if err != nil {
		return nil, fmt.Errorf("capstan: vultr resolve os_id: %w", err)
	}

	body := map[string]any{
		"label":  opts.Name,
		"plan":   opts.Plan,
		"region": opts.Region,
		"os_id":  osID,
	}
	if opts.UserData != "" {
		body["user_data"] = base64.StdEncoding.EncodeToString([]byte(opts.UserData))
	}

	var resp struct {
		Instance vultrInstance `json:"instance"`
	}
	if err := v.http.POST(ctx, v.spec.BaseURL,"/instances", body, &resp); err != nil {
		return nil, err
	}
	return v.toServer(resp.Instance), nil
}

func (v *VultrProvider) Get(ctx context.Context, id string) (*Server, error) {
	var resp struct {
		Instance vultrInstance `json:"instance"`
	}
	if err := v.http.GET(ctx, v.spec.BaseURL,"/instances/"+id, &resp); err != nil {
		return nil, err
	}
	return v.toServer(resp.Instance), nil
}

func (v *VultrProvider) Destroy(ctx context.Context, id string) error {
	return v.http.DELETE(ctx, v.spec.BaseURL,"/instances/"+id)
}

func (v *VultrProvider) EstimateMonthlyCost(plan string) int {
	return v.spec.EstimateMonthlyCost(plan)
}

// List, PowerOn/Off/Restart, WaitForAction will be implemented for
// Vultr as a follow-up. Hetzner is the reference implementation; the
// interface is stable and additive once it lands.

func (v *VultrProvider) List(ctx context.Context, opts ListOpts) ([]Server, error) {
	return nil, ErrNotImplemented
}

func (v *VultrProvider) PowerOn(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (v *VultrProvider) PowerOff(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (v *VultrProvider) Restart(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (v *VultrProvider) WaitForAction(ctx context.Context, actionID string) (*Action, error) {
	return nil, ErrNotImplemented
}

type vultrInstance struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	PowerStatus string `json:"power_status"`
	Plan        string `json:"plan"`
	Region      string `json:"region"`
	MainIP      string `json:"main_ip"`
	V6MainIP    string `json:"v6_main_ip"`
	DateCreated string `json:"date_created"`
}

func (v *VultrProvider) toServer(s vultrInstance) *Server {
	srv := &Server{
		ID:        s.ID,
		Name:      s.Label,
		Status:    v.mapVultrStatus(s.Status, s.PowerStatus),
		Plan:      s.Plan,
		Region:    s.Region,
		CreatedAt: s.DateCreated,
	}
	if s.MainIP != "" && s.MainIP != "0.0.0.0" {
		srv.PublicIPv4 = s.MainIP
	}
	if s.V6MainIP != "" && s.V6MainIP != "::" {
		srv.PublicIPv6 = s.V6MainIP
	}
	return srv
}

// mapVultrStatus maps Vultr's compound status to a ServerStatus.
// When status == "active", the compound key "active/{power_status}" is used.
// Otherwise the simple status key is looked up in the spec.
func (v *VultrProvider) mapVultrStatus(status, powerStatus string) ServerStatus {
	if status == "active" {
		compound := "active/" + powerStatus
		if mapped, ok := v.spec.StatusMap[compound]; ok {
			return ServerStatus(mapped)
		}
		return StatusUnknown
	}
	return v.spec.MapStatus(status)
}

// resolveVultrOSID parses an os_id from an image string (numeric string or integer).
func resolveVultrOSID(image string) (int, error) {
	id, err := strconv.Atoi(image)
	if err != nil {
		return 0, fmt.Errorf("invalid os_id %q: must be numeric", image)
	}
	return id, nil
}

