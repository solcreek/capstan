package capstan

import (
	"context"
	"fmt"
	"time"
)

// sleepCtx sleeps for ms milliseconds or until ctx is cancelled.
func sleepCtx(ctx context.Context, ms int) error {
	t := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type HetznerProvider struct {
	spec *ProviderSpec
	http *httpClient
}

func NewHetzner(token string) *HetznerProvider {
	return &HetznerProvider{
		spec: Spec(Hetzner),
		http: newHTTPClient("hetzner", token),
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
	if err := h.http.GET(ctx, h.spec.BaseURL,"/locations?per_page=50", &resp); err != nil {
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
	if err := h.http.GET(ctx, h.spec.BaseURL,"/server_types?per_page=50", &resp); err != nil {
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
	if err := h.http.POST(ctx, h.spec.BaseURL,"/servers", body, &resp); err != nil {
		return nil, err
	}
	return h.toServer(resp.Server), nil
}

func (h *HetznerProvider) Get(ctx context.Context, id string) (*Server, error) {
	var resp struct {
		Server hetznerServer `json:"server"`
	}
	if err := h.http.GET(ctx, h.spec.BaseURL,"/servers/"+id, &resp); err != nil {
		return nil, err
	}
	return h.toServer(resp.Server), nil
}

func (h *HetznerProvider) List(ctx context.Context, opts ListOpts) ([]Server, error) {
	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 50
	}
	maxServers := opts.MaxServers
	if maxServers <= 0 {
		maxServers = 200
	}

	page := opts.Page
	autoPaginate := page == 0
	if autoPaginate {
		page = 1
	}

	var all []Server
	for {
		path := fmt.Sprintf("/servers?per_page=%d&page=%d", perPage, page)
		if opts.Label != "" {
			path += "&label_selector=" + opts.Label
		}
		var resp struct {
			Servers []hetznerServer `json:"servers"`
			Meta    struct {
				Pagination struct {
					Page         int `json:"page"`
					PerPage      int `json:"per_page"`
					NextPage     *int `json:"next_page"`
					LastPage     int `json:"last_page"`
					TotalEntries int `json:"total_entries"`
				} `json:"pagination"`
			} `json:"meta"`
		}
		if err := h.http.GET(ctx, h.spec.BaseURL,path, &resp); err != nil {
			return nil, err
		}
		for _, s := range resp.Servers {
			all = append(all, *h.toServer(s))
			if len(all) >= maxServers {
				return all, nil
			}
		}
		if !autoPaginate || resp.Meta.Pagination.NextPage == nil {
			return all, nil
		}
		page = *resp.Meta.Pagination.NextPage
	}
}

func (h *HetznerProvider) PowerOn(ctx context.Context, id string) (*Action, error) {
	return h.action(ctx, id, "poweron")
}

func (h *HetznerProvider) PowerOff(ctx context.Context, id string) (*Action, error) {
	return h.action(ctx, id, "poweroff")
}

func (h *HetznerProvider) Restart(ctx context.Context, id string) (*Action, error) {
	// Hetzner: "reboot" is graceful (ACPI shutdown then start), "reset" is hard.
	// We use reboot — closer to user intent of "restart this server."
	return h.action(ctx, id, "reboot")
}

func (h *HetznerProvider) action(ctx context.Context, id, verb string) (*Action, error) {
	var resp struct {
		Action hetznerAction `json:"action"`
	}
	if err := h.http.POST(ctx, h.spec.BaseURL,"/servers/"+id+"/actions/"+verb, map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return h.toAction(resp.Action), nil
}

func (h *HetznerProvider) WaitForAction(ctx context.Context, actionID string) (*Action, error) {
	for {
		var resp struct {
			Action hetznerAction `json:"action"`
		}
		if err := h.http.GET(ctx, h.spec.BaseURL,"/actions/"+actionID, &resp); err != nil {
			return nil, err
		}
		a := h.toAction(resp.Action)
		if a.Status != ActionRunning {
			return a, nil
		}
		// Hetzner publishes progress every ~1s. Caller controls cadence via ctx
		// deadline; we poll on a 500ms tick.
		select {
		case <-ctx.Done():
			return a, ctx.Err()
		default:
		}
		if err := sleepCtx(ctx, 500); err != nil {
			return a, err
		}
	}
}

func (h *HetznerProvider) Destroy(ctx context.Context, id string) error {
	return h.http.DELETE(ctx, h.spec.BaseURL,"/servers/"+id)
}

func (h *HetznerProvider) EstimateMonthlyCost(plan string) int {
	return h.spec.EstimateMonthlyCost(plan)
}

type hetznerAction struct {
	ID       int    `json:"id"`
	Command  string `json:"command"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Started  string `json:"started"`
	Finished string `json:"finished"`
	Error    *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (h *HetznerProvider) toAction(a hetznerAction) *Action {
	act := &Action{
		ID:       fmt.Sprintf("%d", a.ID),
		Command:  a.Command,
		Status:   ActionStatus(a.Status),
		Progress: a.Progress,
		Started:  a.Started,
		Finished: a.Finished,
	}
	if a.Error != nil {
		act.ErrorCode = a.Error.Code
		act.ErrorMsg = a.Error.Message
	}
	return act
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

