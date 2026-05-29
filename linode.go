package capstan

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

type LinodeProvider struct {
	spec *ProviderSpec
	http *httpClient
}

func NewLinode(token string) *LinodeProvider {
	return &LinodeProvider{
		spec: Spec(Linode),
		http: newHTTPClient("linode", token),
	}
}

func (l *LinodeProvider) Name() ProviderName { return Linode }

func (l *LinodeProvider) Regions(ctx context.Context) ([]Region, error) {
	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			Label   string `json:"label"`
			Country string `json:"country"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	if err := l.http.GET(ctx, l.spec.BaseURL,"/regions?page_size=100", &resp); err != nil {
		return nil, err
	}
	var regions []Region
	for _, r := range resp.Data {
		if r.Status != "ok" {
			continue
		}
		regions = append(regions, Region{ID: r.ID, Name: r.Label, Country: r.Country})
	}
	return regions, nil
}

func (l *LinodeProvider) Plans(ctx context.Context, region string) ([]Plan, error) {
	var resp struct {
		Data []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			VCPUs  int    `json:"vcpus"`
			Memory int    `json:"memory"`
			Disk   int    `json:"disk"`
		} `json:"data"`
	}
	if err := l.http.GET(ctx, l.spec.BaseURL,"/linode/types?page_size=100", &resp); err != nil {
		return nil, err
	}
	plans := make([]Plan, len(resp.Data))
	for i, t := range resp.Data {
		plans[i] = Plan{
			ID:            t.ID,
			Name:          t.Label,
			CPUs:          t.VCPUs,
			MemoryMB:      t.Memory,
			DiskGB:        t.Disk / 1024,
			MonthlyCents:  l.spec.EstimateMonthlyCost(t.ID),
			PriceCurrency: l.spec.PriceCurrency,
		}
	}
	return plans, nil
}

func (l *LinodeProvider) Create(ctx context.Context, opts CreateOpts) (*Server, error) {
	rootPass, err := randomRootPass()
	if err != nil {
		return nil, fmt.Errorf("capstan: linode generate root_pass: %w", err)
	}

	body := map[string]any{
		"label":     opts.Name,
		"type":      opts.Plan,
		"region":    opts.Region,
		"image":     l.spec.ResolveImage(opts.Image),
		"root_pass": rootPass,
	}
	if opts.UserData != "" {
		body["metadata"] = map[string]any{
			"user_data": base64.StdEncoding.EncodeToString([]byte(opts.UserData)),
		}
	}

	var s linodeInstance
	if err := l.http.POST(ctx, l.spec.BaseURL,"/linode/instances", body, &s); err != nil {
		return nil, err
	}
	return l.toServer(s), nil
}

func (l *LinodeProvider) Get(ctx context.Context, id string) (*Server, error) {
	var s linodeInstance
	if err := l.http.GET(ctx, l.spec.BaseURL,"/linode/instances/"+id, &s); err != nil {
		return nil, err
	}
	return l.toServer(s), nil
}

func (l *LinodeProvider) Destroy(ctx context.Context, id string) error {
	return l.http.DELETE(ctx, l.spec.BaseURL,"/linode/instances/"+id)
}

func (l *LinodeProvider) EstimateMonthlyCost(plan string) int {
	return l.spec.EstimateMonthlyCost(plan)
}

// List, PowerOn/Off/Restart, WaitForAction will be implemented for
// Linode as a follow-up. Hetzner is the reference implementation; the
// interface is stable and additive once it lands.

func (l *LinodeProvider) List(ctx context.Context, opts ListOpts) ([]Server, error) {
	return nil, ErrNotImplemented
}

func (l *LinodeProvider) PowerOn(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (l *LinodeProvider) PowerOff(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (l *LinodeProvider) Restart(ctx context.Context, id string) (*Action, error) {
	return nil, ErrNotImplemented
}

func (l *LinodeProvider) WaitForAction(ctx context.Context, actionID string) (*Action, error) {
	return nil, ErrNotImplemented
}

type linodeInstance struct {
	ID     int      `json:"id"`
	Label  string   `json:"label"`
	Status string   `json:"status"`
	Type   string   `json:"type"`
	Region string   `json:"region"`
	IPv4   []string `json:"ipv4"`
	IPv6   string   `json:"ipv6"`
	Created string  `json:"created"`
}

func (l *LinodeProvider) toServer(s linodeInstance) *Server {
	srv := &Server{
		ID:        fmt.Sprintf("%d", s.ID),
		Name:      s.Label,
		Status:    l.spec.MapStatus(s.Status),
		Plan:      s.Type,
		Region:    s.Region,
		CreatedAt: s.Created,
	}
	if len(s.IPv4) > 0 {
		srv.PublicIPv4 = s.IPv4[0]
	}
	if s.IPv6 != "" {
		// Strip CIDR prefix if present (e.g. "2001:db8::1/64" -> "2001:db8::1")
		ipv6 := s.IPv6
		for i, c := range ipv6 {
			if c == '/' {
				ipv6 = ipv6[:i]
				break
			}
		}
		srv.PublicIPv6 = ipv6
	}
	return srv
}


// randomRootPass generates a cryptographically random 32-byte base64url string.
func randomRootPass() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
