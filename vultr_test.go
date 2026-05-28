package capstan

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVultrRegions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/regions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth header")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"regions": []map[string]any{
				{"id": "ewr", "city": "New Jersey", "country": "US"},
				{"id": "nrt", "city": "Tokyo", "country": "JP"},
			},
		})
	}))
	defer srv.Close()

	v := NewVultr("test-token")
	v.spec = &ProviderSpec{BaseURL: srv.URL}

	regions, err := v.Regions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("got %d regions", len(regions))
	}
	if regions[0].ID != "ewr" {
		t.Errorf("region[0].ID = %q", regions[0].ID)
	}
	if regions[1].Country != "JP" {
		t.Errorf("region[1].Country = %q", regions[1].Country)
	}
}

func TestVultrCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/instances" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["label"] != "test-vps" {
			t.Errorf("label = %v", body["label"])
		}
		if body["plan"] != "vc2-2c-4gb" {
			t.Errorf("plan = %v", body["plan"])
		}
		if body["region"] != "ewr" {
			t.Errorf("region = %v", body["region"])
		}
		// os_id must be a number
		osID, ok := body["os_id"].(float64)
		if !ok || osID != 2284 {
			t.Errorf("os_id = %v (want 2284)", body["os_id"])
		}

		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]any{
			"instance": map[string]any{
				"id":           "abc-123",
				"label":        "test-vps",
				"status":       "pending",
				"power_status": "running",
				"plan":         "vc2-2c-4gb",
				"region":       "ewr",
				"main_ip":      "0.0.0.0",
				"v6_main_ip":   "::",
				"date_created": "2026-05-28T10:00:00+00:00",
			},
		})
	}))
	defer srv.Close()

	v := NewVultr("test-token")
	v.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "2284",
		StatusMap: map[string]string{
			"pending":        "initializing",
			"active/running": "running",
			"active/stopped": "stopped",
			"active/none":    "initializing",
		},
	}

	s, err := v.Create(context.Background(), CreateOpts{
		Name:   "test-vps",
		Plan:   "vc2-2c-4gb",
		Region: "ewr",
		Image:  "2284",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "abc-123" {
		t.Errorf("ID = %q", s.ID)
	}
	// IPs should be empty for 0.0.0.0 and ::
	if s.PublicIPv4 != "" {
		t.Errorf("IPv4 should be empty for 0.0.0.0, got %q", s.PublicIPv4)
	}
	if s.PublicIPv6 != "" {
		t.Errorf("IPv6 should be empty for ::, got %q", s.PublicIPv6)
	}
	if s.Status != StatusInitializing {
		t.Errorf("Status = %q", s.Status)
	}
}

func TestVultrGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instances/abc-123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"instance": map[string]any{
				"id":           "abc-123",
				"label":        "test-vps",
				"status":       "active",
				"power_status": "running",
				"plan":         "vc2-2c-4gb",
				"region":       "ewr",
				"main_ip":      "1.2.3.4",
				"v6_main_ip":   "2001:db8::1",
				"date_created": "2026-05-28T10:00:00+00:00",
			},
		})
	}))
	defer srv.Close()

	v := NewVultr("test-token")
	v.spec = &ProviderSpec{
		BaseURL: srv.URL,
		StatusMap: map[string]string{
			"active/running": "running",
		},
	}

	s, err := v.Get(context.Background(), "abc-123")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "abc-123" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.PublicIPv4 != "1.2.3.4" {
		t.Errorf("IPv4 = %q", s.PublicIPv4)
	}
	if s.PublicIPv6 != "2001:db8::1" {
		t.Errorf("IPv6 = %q", s.PublicIPv6)
	}
	if s.Status != StatusRunning {
		t.Errorf("Status = %q", s.Status)
	}
}

func TestVultrDestroy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/instances/abc-123" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	v := NewVultr("test-token")
	v.spec = &ProviderSpec{BaseURL: srv.URL}

	if err := v.Destroy(context.Background(), "abc-123"); err != nil {
		t.Fatal(err)
	}
}

func TestVultrErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"Instance not found.","status":404}`))
	}))
	defer srv.Close()

	v := NewVultr("test-token")
	v.spec = &ProviderSpec{BaseURL: srv.URL}

	_, err := v.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVultrDefaultImage(t *testing.T) {
	var receivedOSID float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		receivedOSID, _ = body["os_id"].(float64)
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]any{
			"instance": map[string]any{
				"id": "x", "label": "x", "status": "pending",
				"power_status": "running", "plan": "vc2-1c-1gb", "region": "ewr",
				"main_ip": "0.0.0.0", "v6_main_ip": "::", "date_created": "",
			},
		})
	}))
	defer srv.Close()

	v := NewVultr("test-token")
	v.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "2284",
		StatusMap:    map[string]string{"pending": "initializing"},
	}

	v.Create(context.Background(), CreateOpts{Name: "x", Plan: "vc2-1c-1gb", Region: "ewr"})
	if receivedOSID != 2284 {
		t.Errorf("default os_id = %v, want 2284", receivedOSID)
	}
}

func TestVultrUserDataBase64Encoded(t *testing.T) {
	const userData = "#cloud-config\nhostname: test\n"
	var receivedUserData string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		receivedUserData, _ = body["user_data"].(string)
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]any{
			"instance": map[string]any{
				"id": "x", "label": "x", "status": "pending",
				"power_status": "running", "plan": "vc2-1c-1gb", "region": "ewr",
				"main_ip": "0.0.0.0", "v6_main_ip": "::", "date_created": "",
			},
		})
	}))
	defer srv.Close()

	v := NewVultr("test-token")
	v.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "2284",
		StatusMap:    map[string]string{"pending": "initializing"},
	}

	v.Create(context.Background(), CreateOpts{
		Name:     "x",
		Plan:     "vc2-1c-1gb",
		Region:   "ewr",
		UserData: userData,
	})
	if receivedUserData == "" {
		t.Fatal("user_data not sent")
	}
	decoded, err := base64.StdEncoding.DecodeString(receivedUserData)
	if err != nil {
		t.Fatalf("user_data is not valid base64: %v", err)
	}
	if string(decoded) != userData {
		t.Errorf("decoded user_data = %q, want %q", decoded, userData)
	}
}

func TestVultrCompoundStatus(t *testing.T) {
	tests := []struct {
		status      string
		powerStatus string
		want        ServerStatus
	}{
		{"active", "running", StatusRunning},
		{"active", "stopped", StatusStopped},
		{"active", "none", StatusInitializing},
		{"pending", "", StatusInitializing},
	}

	v := &VultrProvider{
		spec: &ProviderSpec{
			StatusMap: map[string]string{
				"pending":        "initializing",
				"active/running": "running",
				"active/stopped": "stopped",
				"active/none":    "initializing",
			},
		},
	}

	for _, tt := range tests {
		got := v.mapVultrStatus(tt.status, tt.powerStatus)
		if got != tt.want {
			t.Errorf("mapVultrStatus(%q, %q) = %q, want %q", tt.status, tt.powerStatus, got, tt.want)
		}
	}
}
