// Package capstan provides a multi-provider VPS lifecycle abstraction.
//
// Provider metadata (base URLs, status mappings, pricing) is loaded from
// shared JSON specs in ../specs/. The same specs are used by the TypeScript
// implementation, ensuring both languages share a single source of truth.
package capstan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNotImplemented is returned by provider methods that are not yet wired up
// for that provider. The interface is unified across providers so consumers
// (dew, Marina-via-dew, bench tools) can write provider-agnostic code; some
// methods land on a per-provider schedule and stub out until then.
var ErrNotImplemented = errors.New("capstan: not implemented for this provider")

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

	// Servers — read.
	List(ctx context.Context, opts ListOpts) ([]Server, error)
	Get(ctx context.Context, id string) (*Server, error)

	// Servers — lifecycle.
	Create(ctx context.Context, opts CreateOpts) (*Server, error)
	Destroy(ctx context.Context, id string) error

	// Servers — power actions. Return an Action immediately; the action
	// itself completes asynchronously. Use WaitForAction to block until
	// terminal status, or fire-and-forget for snappier UI feedback.
	PowerOn(ctx context.Context, id string) (*Action, error)
	PowerOff(ctx context.Context, id string) (*Action, error)
	Restart(ctx context.Context, id string) (*Action, error)

	// WaitForAction polls until the action reaches a terminal status
	// (success or error). Caller decides the polling cadence via ctx.
	WaitForAction(ctx context.Context, actionID string) (*Action, error)

	EstimateMonthlyCost(plan string) int
}

// ListOpts controls pagination and filtering for Provider.List.
//
// Page == 0 means "auto-paginate up to MaxServers servers" (default 200).
// PerPage is provider-dependent; capstan picks a sensible default if unset.
type ListOpts struct {
	Page       int
	PerPage    int
	MaxServers int
	Label      string // optional provider-specific tag filter, "" = none
}

type ActionStatus string

const (
	ActionRunning ActionStatus = "running"
	ActionSuccess ActionStatus = "success"
	ActionError   ActionStatus = "error"
)

type Action struct {
	ID        string       `json:"id"`
	Command   string       `json:"command"`             // e.g. "start_server", "stop_server", "reboot"
	Status    ActionStatus `json:"status"`
	Progress  int          `json:"progress"`            // 0-100; not all providers populate this
	Started   string       `json:"started,omitempty"`
	Finished  string       `json:"finished,omitempty"`
	ErrorCode string       `json:"errorCode,omitempty"`
	ErrorMsg  string       `json:"errorMessage,omitempty"`
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

// New constructs a Provider by name. Returns an error when the name is not
// one of the supported providers. Mirrors the TypeScript registry's
// createProvider helper so downstream consumers (dew, bench tools) can write
// provider-agnostic code without a per-call type switch.
func New(name ProviderName, token string) (Provider, error) {
	switch name {
	case Hetzner:
		return NewHetzner(token), nil
	case DigitalOcean:
		return NewDigitalOcean(token), nil
	case Linode:
		return NewLinode(token), nil
	case Vultr:
		return NewVultr(token), nil
	default:
		return nil, fmt.Errorf("capstan: unknown provider %q", name)
	}
}
