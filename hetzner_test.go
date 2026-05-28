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
