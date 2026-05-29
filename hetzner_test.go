package capstan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHetznerRegions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/locations" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth header")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"locations": []map[string]any{
				{"id": 1, "name": "fsn1", "description": "Falkenstein", "country": "DE", "city": "Falkenstein"},
				{"id": 2, "name": "nbg1", "description": "Nuremberg", "country": "DE", "city": "Nuremberg"},
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL}

	regions, err := h.Regions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("got %d regions", len(regions))
	}
	if regions[0].ID != "fsn1" {
		t.Errorf("region[0].ID = %q", regions[0].ID)
	}
	if regions[1].Name != "Nuremberg" {
		t.Errorf("region[1].Name = %q", regions[1].Name)
	}
}

func TestHetznerCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/servers" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "test-vps" {
			t.Errorf("name = %v", body["name"])
		}
		if body["server_type"] != "cx33" {
			t.Errorf("server_type = %v", body["server_type"])
		}
		if body["location"] != "fsn1" {
			t.Errorf("location = %v", body["location"])
		}
		if body["image"] != "debian-12" {
			t.Errorf("image = %v", body["image"])
		}
		if body["user_data"] != "#cloud-config\n" {
			t.Errorf("user_data = %v", body["user_data"])
		}

		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{
			"server": map[string]any{
				"id":     42,
				"name":   "test-vps",
				"status": "initializing",
				"public_net": map[string]any{
					"ipv4": map[string]any{"ip": "1.2.3.4"},
					"ipv6": map[string]any{"ip": "2001:db8::1"},
				},
				"server_type": map[string]any{"name": "cx33"},
				"datacenter":  map[string]any{"location": map[string]any{"name": "fsn1"}},
				"created":     "2026-05-28T10:00:00+00:00",
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "ubuntu-24.04",
		StatusMap:    map[string]string{"initializing": "initializing"},
	}

	s, err := h.Create(context.Background(), CreateOpts{
		Name:     "test-vps",
		Plan:     "cx33",
		Region:   "fsn1",
		Image:    "debian-12",
		UserData: "#cloud-config\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "42" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.PublicIPv4 != "1.2.3.4" {
		t.Errorf("IPv4 = %q", s.PublicIPv4)
	}
	if s.Status != StatusInitializing {
		t.Errorf("Status = %q", s.Status)
	}
	if s.Region != "fsn1" {
		t.Errorf("Region = %q", s.Region)
	}
}

func TestHetznerGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/servers/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"server": map[string]any{
				"id":     42,
				"name":   "test-vps",
				"status": "running",
				"public_net": map[string]any{
					"ipv4": map[string]any{"ip": "1.2.3.4"},
				},
				"server_type": map[string]any{"name": "cx33"},
				"datacenter":  map[string]any{"location": map[string]any{"name": "fsn1"}},
				"created":     "2026-05-28T10:00:00+00:00",
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{
		BaseURL:   srv.URL,
		StatusMap: map[string]string{"running": "running"},
	}

	s, err := h.Get(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusRunning {
		t.Errorf("Status = %q", s.Status)
	}
}

func TestHetznerDestroy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/servers/42" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL}

	if err := h.Destroy(context.Background(), "42"); err != nil {
		t.Fatal(err)
	}
}

func TestHetznerErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"code":"not_found","message":"server not found"}}`))
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL}

	_, err := h.Get(context.Background(), "999")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHetznerDefaultImage(t *testing.T) {
	var receivedImage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		receivedImage = body["image"].(string)
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{
			"server": map[string]any{
				"id": 1, "name": "x", "status": "initializing", "created": "",
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "ubuntu-24.04",
		StatusMap:    map[string]string{"initializing": "initializing"},
	}

	h.Create(context.Background(), CreateOpts{Name: "x", Plan: "cx33", Region: "fsn1"})
	if receivedImage != "ubuntu-24.04" {
		t.Errorf("default image = %q, want ubuntu-24.04", receivedImage)
	}
}

func TestHetznerList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/servers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"servers": []map[string]any{
				{"id": 1, "name": "web-1", "status": "running", "created": "2026-05-28T10:00:00+00:00"},
				{"id": 2, "name": "db-1", "status": "off", "created": "2026-05-28T11:00:00+00:00"},
			},
			"meta": map[string]any{
				"pagination": map[string]any{
					"page": 1, "per_page": 50, "next_page": nil, "last_page": 1, "total_entries": 2,
				},
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{
		BaseURL:   srv.URL,
		StatusMap: map[string]string{"running": "running", "off": "stopped"},
	}

	servers, err := h.List(context.Background(), ListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("got %d servers", len(servers))
	}
	if servers[0].Name != "web-1" || servers[0].Status != StatusRunning {
		t.Errorf("server[0] = %+v", servers[0])
	}
	if servers[1].Status != StatusStopped {
		t.Errorf("server[1].Status = %q", servers[1].Status)
	}
}

func TestHetznerListAutoPaginate(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		if page == 1 {
			next := 2
			json.NewEncoder(w).Encode(map[string]any{
				"servers": []map[string]any{
					{"id": 1, "name": "a", "status": "running", "created": ""},
					{"id": 2, "name": "b", "status": "running", "created": ""},
				},
				"meta": map[string]any{
					"pagination": map[string]any{"page": 1, "next_page": next, "last_page": 2, "total_entries": 3},
				},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"servers": []map[string]any{
					{"id": 3, "name": "c", "status": "running", "created": ""},
				},
				"meta": map[string]any{
					"pagination": map[string]any{"page": 2, "next_page": nil, "last_page": 2, "total_entries": 3},
				},
			})
		}
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL, StatusMap: map[string]string{"running": "running"}}

	servers, err := h.List(context.Background(), ListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 3 {
		t.Fatalf("auto-paginate got %d servers, want 3", len(servers))
	}
	if page != 2 {
		t.Errorf("expected 2 page requests, got %d", page)
	}
}

func TestHetznerListMaxServersCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"servers": []map[string]any{
				{"id": 1, "name": "a", "status": "running", "created": ""},
				{"id": 2, "name": "b", "status": "running", "created": ""},
				{"id": 3, "name": "c", "status": "running", "created": ""},
			},
			"meta": map[string]any{
				"pagination": map[string]any{"page": 1, "next_page": nil, "last_page": 1, "total_entries": 3},
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL, StatusMap: map[string]string{"running": "running"}}

	servers, err := h.List(context.Background(), ListOpts{MaxServers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("MaxServers=2 got %d servers", len(servers))
	}
}

func TestHetznerPowerOn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/servers/42/actions/poweron" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"action": map[string]any{
				"id":       7,
				"command":  "start_server",
				"status":   "running",
				"progress": 0,
				"started":  "2026-05-29T10:00:00+00:00",
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL}

	a, err := h.PowerOn(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "7" {
		t.Errorf("Action.ID = %q", a.ID)
	}
	if a.Command != "start_server" {
		t.Errorf("Action.Command = %q", a.Command)
	}
	if a.Status != ActionRunning {
		t.Errorf("Action.Status = %q", a.Status)
	}
}

func TestHetznerPowerOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/servers/42/actions/poweroff" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"action": map[string]any{
				"id":      8,
				"command": "stop_server",
				"status":  "running",
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL}

	if _, err := h.PowerOff(context.Background(), "42"); err != nil {
		t.Fatal(err)
	}
}

func TestHetznerRestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/servers/42/actions/reboot" {
			t.Errorf("expected reboot, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"action": map[string]any{
				"id": 9, "command": "reboot_server", "status": "running",
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL}

	if _, err := h.Restart(context.Background(), "42"); err != nil {
		t.Fatal(err)
	}
}

func TestHetznerWaitForActionSuccess(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/actions/7" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		polls++
		// First two polls report running, then success.
		if polls < 3 {
			json.NewEncoder(w).Encode(map[string]any{
				"action": map[string]any{
					"id": 7, "command": "start_server", "status": "running", "progress": polls * 33,
				},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"action": map[string]any{
					"id": 7, "command": "start_server", "status": "success",
					"progress": 100, "finished": "2026-05-29T10:00:02+00:00",
				},
			})
		}
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL}

	a, err := h.WaitForAction(context.Background(), "7")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != ActionSuccess {
		t.Errorf("Status = %q", a.Status)
	}
	if polls < 3 {
		t.Errorf("expected ≥3 polls, got %d", polls)
	}
}

func TestHetznerWaitForActionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"action": map[string]any{
				"id": 7, "command": "start_server", "status": "error",
				"error": map[string]any{
					"code": "server_locked", "message": "server is locked",
				},
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{BaseURL: srv.URL}

	a, err := h.WaitForAction(context.Background(), "7")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != ActionError {
		t.Errorf("Status = %q", a.Status)
	}
	if a.ErrorCode != "server_locked" {
		t.Errorf("ErrorCode = %q", a.ErrorCode)
	}
}

func TestHetznerUserDataOmittedWhenEmpty(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{
			"server": map[string]any{
				"id": 1, "name": "x", "status": "initializing", "created": "",
			},
		})
	}))
	defer srv.Close()

	h := NewHetzner("test-token")
	h.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "ubuntu-24.04",
		StatusMap:    map[string]string{"initializing": "initializing"},
	}

	h.Create(context.Background(), CreateOpts{Name: "x", Plan: "cx33", Region: "fsn1"})
	if _, ok := body["user_data"]; ok {
		t.Error("user_data should be omitted when empty")
	}
}
