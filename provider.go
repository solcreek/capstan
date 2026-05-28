// Package capstan provides a multi-provider VPS lifecycle abstraction.
//
// Provider metadata (base URLs, status mappings, pricing) is loaded from
// shared JSON specs in ../specs/. The same specs are used by the TypeScript
// implementation, ensuring both languages share a single source of truth.
package capstan

import (
	"context"
	"encoding/json"
	"fmt"
)

type ProviderName string

const (
	Hetzner      ProviderName = "hetzner"
	DigitalOcean ProviderName = "digitalocean"
	Linode       ProviderName = "linode"
	Vultr        ProviderName = "vultr"
)

type Provider interface {
	Name() ProviderName
	Regions(ctx context.Context) ([]Region, error)
	Plans(ctx context.Context, region string) ([]Plan, error)
	Create(ctx context.Context, opts CreateOpts) (*Server, error)
	Get(ctx context.Context, id string) (*Server, error)
	Destroy(ctx context.Context, id string) error
	EstimateMonthlyCost(plan string) int
}

type Region struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country,omitempty"`
	City    string `json:"city,omitempty"`
}

type Plan struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	CPUs            int    `json:"cpus"`
	MemoryMB        int    `json:"memoryMb"`
	DiskGB          int    `json:"diskGb"`
	MonthlyCents    int    `json:"monthlyCents"`
	PriceCurrency   string `json:"priceCurrency"`
}

type CreateOpts struct {
	Name     string
	Region   string
	Plan     string
	Image    string
	UserData string
}

type ServerStatus string

const (
	StatusInitializing ServerStatus = "initializing"
	StatusRunning      ServerStatus = "running"
	StatusStopped      ServerStatus = "stopped"
	StatusDeleting     ServerStatus = "deleting"
	StatusUnknown      ServerStatus = "unknown"
)

type Server struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Status     ServerStatus `json:"status"`
	PublicIPv4 string       `json:"publicIpv4,omitempty"`
	PublicIPv6 string       `json:"publicIpv6,omitempty"`
	Region     string       `json:"region"`
	Plan       string       `json:"plan"`
	CreatedAt  string       `json:"createdAt"`
}

// ProviderSpec holds metadata loaded from specs/*.json.
type ProviderSpec struct {
	Name             string            `json:"name"`
	DisplayName      string            `json:"displayName"`
	BaseURL          string            `json:"baseUrl"`
	DefaultImage     string            `json:"defaultImage"`
	UserDataEncoding string            `json:"userDataEncoding"`
	StatusMap        map[string]string `json:"statusMap"`
	PriceCents       map[string]int    `json:"priceCents"`
	PriceCurrency    string            `json:"priceCurrency"`
}

func LoadSpec(data []byte) (*ProviderSpec, error) {
	var s ProviderSpec
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("capstan: parse spec: %w", err)
	}
	return &s, nil
}

func (s *ProviderSpec) MapStatus(raw string) ServerStatus {
	if v, ok := s.StatusMap[raw]; ok {
		return ServerStatus(v)
	}
	return StatusUnknown
}

func (s *ProviderSpec) EstimateMonthlyCost(plan string) int {
	return s.PriceCents[plan]
}

func (s *ProviderSpec) ResolveImage(image string) string {
	if image != "" {
		return image
	}
	return s.DefaultImage
}

// AllProviders returns the names of all supported providers.
func AllProviders() []ProviderName {
	return []ProviderName{Hetzner, DigitalOcean, Linode, Vultr}
}
