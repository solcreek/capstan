package capstan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type HetznerProvider struct {
	token  string
	spec   *ProviderSpec
	client *http.Client
}

func NewHetzner(token string) *HetznerProvider {
	return &HetznerProvider{
		token:  token,
		spec:   Spec(Hetzner),
		client: &http.Client{},
	}
}

func (h *HetznerProvider) Name() ProviderName { return Hetzner }

func (h *HetznerProvider) Regions(ctx context.Context) ([]Region, error) {
	var resp struct {
		Locations []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Country     string `json:"country"`
			City        string `json:"city"`
		} `json:"locations"`
	}
	if err := h.get(ctx, "/locations?per_page=50", &resp); err != nil {
		return nil, err
	}
	regions := make([]Region, len(resp.Locations))
	for i, l := range resp.Locations {
		regions[i] = Region{ID: l.Name, Name: l.Description, Country: l.Country, City: l.City}
	}
	return regions, nil
}

func (h *HetznerProvider) Plans(ctx context.Context, region string) ([]Plan, error) {
	var resp struct {
		ServerTypes []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Cores       int    `json:"cores"`
			Memory      int    `json:"memory"`
			Disk        int    `json:"disk"`
			Prices      []struct {
				Location     string `json:"location"`
				PriceMonthly struct {
					Gross string `json:"gross"`
				} `json:"price_monthly"`
			} `json:"prices"`
		} `json:"server_types"`
	}
	if err := h.get(ctx, "/server_types?per_page=50", &resp); err != nil {
		return nil, err
	}
	var plans []Plan
	for _, t := range resp.ServerTypes {
		if region != "" {
			found := false
			for _, p := range t.Prices {
				if p.Location == region {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		plans = append(plans, Plan{
			ID:            t.Name,
			Name:          t.Description,
			CPUs:          t.Cores,
			MemoryMB:      t.Memory * 1024,
			DiskGB:        t.Disk,
			MonthlyCents:  h.spec.EstimateMonthlyCost(t.Name),
			PriceCurrency: h.spec.PriceCurrency,
		})
	}
	return plans, nil
}

func (h *HetznerProvider) Create(ctx context.Context, opts CreateOpts) (*Server, error) {
	body := map[string]any{
		"name":               opts.Name,
		"server_type":        opts.Plan,
		"location":           opts.Region,
		"image":              h.spec.ResolveImage(opts.Image),
		"start_after_create": true,
	}
	if opts.UserData != "" {
		body["user_data"] = opts.UserData
	}

	var resp struct {
		Server hetznerServer `json:"server"`
	}
	if err := h.post(ctx, "/servers", body, &resp); err != nil {
		return nil, err
	}
	return h.toServer(resp.Server), nil
}

func (h *HetznerProvider) Get(ctx context.Context, id string) (*Server, error) {
	var resp struct {
		Server hetznerServer `json:"server"`
	}
	if err := h.get(ctx, "/servers/"+id, &resp); err != nil {
		return nil, err
	}
	return h.toServer(resp.Server), nil
}

func (h *HetznerProvider) Destroy(ctx context.Context, id string) error {
	return h.del(ctx, "/servers/"+id)
}

func (h *HetznerProvider) EstimateMonthlyCost(plan string) int {
	return h.spec.EstimateMonthlyCost(plan)
}

type hetznerServer struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	PublicNet *struct {
		IPv4 *struct{ IP string `json:"ip"` } `json:"ipv4"`
		IPv6 *struct{ IP string `json:"ip"` } `json:"ipv6"`
	} `json:"public_net"`
	ServerType *struct{ Name string `json:"name"` } `json:"server_type"`
	Datacenter *struct {
		Location *struct{ Name string `json:"name"` } `json:"location"`
	} `json:"datacenter"`
	Created string `json:"created"`
}

func (h *HetznerProvider) toServer(s hetznerServer) *Server {
	srv := &Server{
		ID:        fmt.Sprintf("%d", s.ID),
		Name:      s.Name,
		Status:    h.spec.MapStatus(s.Status),
		CreatedAt: s.Created,
	}
	if s.PublicNet != nil && s.PublicNet.IPv4 != nil {
		srv.PublicIPv4 = s.PublicNet.IPv4.IP
	}
	if s.PublicNet != nil && s.PublicNet.IPv6 != nil {
		srv.PublicIPv6 = s.PublicNet.IPv6.IP
	}
	if s.ServerType != nil {
		srv.Plan = s.ServerType.Name
	}
	if s.Datacenter != nil && s.Datacenter.Location != nil {
		srv.Region = s.Datacenter.Location.Name
	}
	return srv
}

func (h *HetznerProvider) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", h.spec.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Accept", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("capstan: hetzner GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("capstan: hetzner GET %s: %d %s", path, resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

func (h *HetznerProvider) post(ctx context.Context, path string, payload any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", h.spec.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("capstan: hetzner POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("capstan: hetzner POST %s: %d %s", path, resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

func (h *HetznerProvider) del(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", h.spec.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("capstan: hetzner DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("capstan: hetzner DELETE %s: %d %s", path, resp.StatusCode, body)
	}
	return nil
}
