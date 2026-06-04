package capstan

import (
	"context"
	"fmt"
)

type DigitalOceanProvider struct {
	spec *ProviderSpec
	http *httpClient
}

func NewDigitalOcean(token string) *DigitalOceanProvider {
	return &DigitalOceanProvider{
		spec: Spec(DigitalOcean),
		http: newHTTPClient("digitalocean", token),
	}
}

func (d *DigitalOceanProvider) Name() ProviderName { return DigitalOcean }

func (d *DigitalOceanProvider) Regions(ctx context.Context) ([]Region, error) {
	var resp struct {
		Regions []struct {
			Slug      string `json:"slug"`
			Name      string `json:"name"`
			Available bool   `json:"available"`
		} `json:"regions"`
	}
	if err := d.http.GET(ctx, d.spec.BaseURL,"/regions?per_page=50", &resp); err != nil {
		return nil, err
	}
	var regions []Region
	for _, r := range resp.Regions {
		if !r.Available {
			continue
		}
		regions = append(regions, Region{ID: r.Slug, Name: r.Name})
	}
	return regions, nil
}

func (d *DigitalOceanProvider) Plans(ctx context.Context, region string) ([]Plan, error) {
	var resp struct {
		Sizes []struct {
			Slug      string  `json:"slug"`
			Available bool    `json:"available"`
			VCPUs     int     `json:"vcpus"`
			Memory    int     `json:"memory"`
			Disk      int     `json:"disk"`
			PriceMonthly float64 `json:"price_monthly"`
			Regions   []string `json:"regions"`
		} `json:"sizes"`
	}
	if err := d.http.GET(ctx, d.spec.BaseURL,"/sizes?per_page=200", &resp); err != nil {
		return nil, err
	}
	var plans []Plan
	for _, s := range resp.Sizes {
		if !s.Available {
			continue
		}
		if region != "" {
			found := false
			for _, r := range s.Regions {
				if r == region {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		plans = append(plans, Plan{
			ID:              s.Slug,
			Name:            s.Slug,
			CPUs:            s.VCPUs,
			MemoryMB:        s.Memory,
			DiskGB:          s.Disk,
			MonthlyCents:    d.spec.EstimateMonthlyCost(s.Slug),
			PriceCurrency:   d.spec.PriceCurrency,
			APIMonthlyCents: dollarsToCents(s.PriceMonthly),
		})
	}
	return plans, nil
}

func (d *DigitalOceanProvider) Create(ctx context.Context, opts CreateOpts) (*Server, error) {
	body := map[string]any{
		"name":       opts.Name,
		"size":       opts.Plan,
		"region":     opts.Region,
		"image":      d.spec.ResolveImage(opts.Image),
		"monitoring": true,
	}
	if opts.UserData != "" {
		body["user_data"] = opts.UserData
	}

	var resp struct {
		Droplet doDroplet `json:"droplet"`
	}
	if err := d.http.POST(ctx, d.spec.BaseURL,"/droplets", body, &resp); err != nil {
		return nil, err
	}
	return d.toServer(resp.Droplet), nil
}

func (d *DigitalOceanProvider) Get(ctx context.Context, id string) (*Server, error) {
	var resp struct {
		Droplet doDroplet `json:"droplet"`
	}
	if err := d.http.GET(ctx, d.spec.BaseURL,"/droplets/"+id, &resp); err != nil {
		return nil, err
	}
	return d.toServer(resp.Droplet), nil
}

func (d *DigitalOceanProvider) Destroy(ctx context.Context, id string) error {
	return d.http.DELETE(ctx, d.spec.BaseURL,"/droplets/"+id)
}

func (d *DigitalOceanProvider) EstimateMonthlyCost(plan string) int {
	return d.spec.EstimateMonthlyCost(plan)
}

// List, PowerOn/Off/Restart, WaitForAction will be implemented for
// DigitalOcean as a follow-up. Hetzner is the reference implementation; the
// interface is stable and additive once it lands.

func (d *DigitalOceanProvider) List(ctx context.Context, opts ListOpts) ([]Server, error) {
	return nil, ErrNotImplemented
}

func (d *DigitalOceanProvider) PowerOn(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (d *DigitalOceanProvider) PowerOff(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (d *DigitalOceanProvider) Restart(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (d *DigitalOceanProvider) WaitForAction(ctx context.Context, actionID string) (*Action, error) {
	return nil, ErrNotImplemented
}

type doDroplet struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Networks *struct {
		V4 []struct {
			IPAddress string `json:"ip_address"`
			Type      string `json:"type"`
		} `json:"v4"`
		V6 []struct {
			IPAddress string `json:"ip_address"`
			Type      string `json:"type"`
		} `json:"v6"`
	} `json:"networks"`
	Size   *struct{ Slug string `json:"slug"` } `json:"size"`
	Region *struct{ Slug string `json:"slug"` } `json:"region"`
	Created string `json:"created_at"`
}

func (d *DigitalOceanProvider) toServer(s doDroplet) *Server {
	srv := &Server{
		ID:        fmt.Sprintf("%d", s.ID),
		Name:      s.Name,
		Status:    d.spec.MapStatus(s.Status),
		CreatedAt: s.Created,
	}
	if s.Networks != nil {
		for _, n := range s.Networks.V4 {
			if n.Type == "public" {
				srv.PublicIPv4 = n.IPAddress
				break
			}
		}
		for _, n := range s.Networks.V6 {
			if n.Type == "public" {
				srv.PublicIPv6 = n.IPAddress
				break
			}
		}
	}
	if s.Size != nil {
		srv.Plan = s.Size.Slug
	}
	if s.Region != nil {
		srv.Region = s.Region.Slug
	}
	return srv
}

