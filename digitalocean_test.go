package capstan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDigitalOceanRegions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/regions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth header")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"regions": []map[string]any{
				{"slug": "nyc1", "name": "New York 1", "available": true},
				{"slug": "sfo2", "name": "San Francisco 2", "available": true},
				{"slug": "ams2", "name": "Amsterdam 2", "available": false},
			},
		})
	}))
	defer srv.Close()

	d := NewDigitalOcean("test-token")
	d.spec = &ProviderSpec{BaseURL: srv.URL}

	regions, err := d.Regions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("got %d regions, want 2 (unavailable filtered)", len(regions))
	}
	if regions[0].ID != "nyc1" {
		t.Errorf("region[0].ID = %q", regions[0].ID)
	}
	if regions[1].Name != "San Francisco 2" {
		t.Errorf("region[1].Name = %q", regions[1].Name)
	}
}

func TestDigitalOceanCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/droplets" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "test-vps" {
			t.Errorf("name = %v", body["name"])
		}
		if body["size"] != "s-2vcpu-4gb" {
			t.Errorf("size = %v", body["size"])
		}
		if body["region"] != "nyc1" {
			t.Errorf("region = %v", body["region"])
		}
		if body["image"] != "ubuntu-24-04-x64" {
			t.Errorf("image = %v", body["image"])
		}
		if body["monitoring"] != true {
			t.Errorf("monitoring = %v", body["monitoring"])
		}
		if body["user_data"] != "#cloud-config\n" {
			t.Errorf("user_data = %v", body["user_data"])
		}

		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]any{
			"droplet": map[string]any{
				"id":     101,
				"name":   "test-vps",
				"status": "new",
				"networks": map[string]any{
					"v4": []map[string]any{
						{"ip_address": "10.0.0.1", "type": "private"},
						{"ip_address": "1.2.3.4", "type": "public"},
					},
					"v6": []map[string]any{
						{"ip_address": "2001:db8::1", "type": "public"},
					},
				},
				"size":       map[string]any{"slug": "s-2vcpu-4gb"},
				"region":     map[string]any{"slug": "nyc1"},
				"created_at": "2026-05-28T10:00:00Z",
			},
		})
	}))
	defer srv.Close()

	d := NewDigitalOcean("test-token")
	d.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "ubuntu-24-04-x64",
		StatusMap:    map[string]string{"new": "initializing"},
	}

	s, err := d.Create(context.Background(), CreateOpts{
		Name:     "test-vps",
		Plan:     "s-2vcpu-4gb",
		Region:   "nyc1",
		Image:    "ubuntu-24-04-x64",
		UserData: "#cloud-config\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "101" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.PublicIPv4 != "1.2.3.4" {
		t.Errorf("IPv4 = %q", s.PublicIPv4)
	}
	if s.PublicIPv6 != "2001:db8::1" {
		t.Errorf("IPv6 = %q", s.PublicIPv6)
	}
	if s.Status != StatusInitializing {
		t.Errorf("Status = %q", s.Status)
	}
	if s.Region != "nyc1" {
		t.Errorf("Region = %q", s.Region)
	}
	if s.Plan != "s-2vcpu-4gb" {
		t.Errorf("Plan = %q", s.Plan)
	}
}

func TestDigitalOceanGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/droplets/101" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"droplet": map[string]any{
				"id":     101,
				"name":   "test-vps",
				"status": "active",
				"networks": map[string]any{
					"v4": []map[string]any{
						{"ip_address": "1.2.3.4", "type": "public"},
					},
				},
				"size":       map[string]any{"slug": "s-2vcpu-4gb"},
				"region":     map[string]any{"slug": "nyc1"},
				"created_at": "2026-05-28T10:00:00Z",
			},
		})
	}))
	defer srv.Close()

	d := NewDigitalOcean("test-token")
	d.spec = &ProviderSpec{
		BaseURL:   srv.URL,
		StatusMap: map[string]string{"active": "running"},
	}

	s, err := d.Get(context.Background(), "101")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "101" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.Status != StatusRunning {
		t.Errorf("Status = %q", s.Status)
	}
}

func TestDigitalOceanDestroy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/droplets/101" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	d := NewDigitalOcean("test-token")
	d.spec = &ProviderSpec{BaseURL: srv.URL}

	if err := d.Destroy(context.Background(), "101"); err != nil {
		t.Fatal(err)
	}
}

func TestDigitalOceanErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"id":"not_found","message":"The resource you were accessing could not be found."}`))
	}))
	defer srv.Close()

	d := NewDigitalOcean("test-token")
	d.spec = &ProviderSpec{BaseURL: srv.URL}

	_, err := d.Get(context.Background(), "999")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDigitalOceanDefaultImage(t *testing.T) {
	var receivedImage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		receivedImage, _ = body["image"].(string)
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]any{
			"droplet": map[string]any{
				"id": 1, "name": "x", "status": "new", "created_at": "",
			},
		})
	}))
	defer srv.Close()

	d := NewDigitalOcean("test-token")
	d.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "ubuntu-24-04-x64",
		StatusMap:    map[string]string{"new": "initializing"},
	}

	d.Create(context.Background(), CreateOpts{Name: "x", Plan: "s-1vcpu-1gb", Region: "nyc1"})
	if receivedImage != "ubuntu-24-04-x64" {
		t.Errorf("default image = %q, want ubuntu-24-04-x64", receivedImage)
	}
}

func TestDigitalOceanUserDataOmittedWhenEmpty(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]any{
			"droplet": map[string]any{
				"id": 1, "name": "x", "status": "new", "created_at": "",
			},
		})
	}))
	defer srv.Close()

	d := NewDigitalOcean("test-token")
	d.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "ubuntu-24-04-x64",
		StatusMap:    map[string]string{"new": "initializing"},
	}

	d.Create(context.Background(), CreateOpts{Name: "x", Plan: "s-1vcpu-1gb", Region: "nyc1"})
	if _, ok := body["user_data"]; ok {
		t.Error("user_data should be omitted when empty")
	}
}
