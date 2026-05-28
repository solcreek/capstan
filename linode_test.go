package capstan

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinodeRegions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/regions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing auth header")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "us-east", "label": "Newark, NJ, USA", "country": "us", "status": "ok"},
				{"id": "ap-south", "label": "Singapore, SG", "country": "sg", "status": "ok"},
				{"id": "eu-west", "label": "London, UK", "country": "uk", "status": "degraded"},
			},
		})
	}))
	defer srv.Close()

	l := NewLinode("test-token")
	l.spec = &ProviderSpec{BaseURL: srv.URL}

	regions, err := l.Regions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("got %d regions, want 2 (degraded filtered)", len(regions))
	}
	if regions[0].ID != "us-east" {
		t.Errorf("region[0].ID = %q", regions[0].ID)
	}
	if regions[1].Country != "sg" {
		t.Errorf("region[1].Country = %q", regions[1].Country)
	}
}

func TestLinodeCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/linode/instances" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["label"] != "test-vps" {
			t.Errorf("label = %v", body["label"])
		}
		if body["type"] != "g6-standard-2" {
			t.Errorf("type = %v", body["type"])
		}
		if body["region"] != "us-east" {
			t.Errorf("region = %v", body["region"])
		}
		if body["image"] != "linode/ubuntu24.04" {
			t.Errorf("image = %v", body["image"])
		}
		if body["root_pass"] == "" {
			t.Error("root_pass should not be empty")
		}

		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"id":      201,
			"label":   "test-vps",
			"status":  "provisioning",
			"type":    "g6-standard-2",
			"region":  "us-east",
			"ipv4":    []string{"1.2.3.4"},
			"ipv6":    "2001:db8::1/64",
			"created": "2026-05-28T10:00:00",
		})
	}))
	defer srv.Close()

	l := NewLinode("test-token")
	l.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "linode/ubuntu24.04",
		StatusMap:    map[string]string{"provisioning": "initializing"},
	}

	s, err := l.Create(context.Background(), CreateOpts{
		Name:   "test-vps",
		Plan:   "g6-standard-2",
		Region: "us-east",
		Image:  "linode/ubuntu24.04",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "201" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.PublicIPv4 != "1.2.3.4" {
		t.Errorf("IPv4 = %q", s.PublicIPv4)
	}
	if s.PublicIPv6 != "2001:db8::1" {
		t.Errorf("IPv6 = %q (CIDR should be stripped)", s.PublicIPv6)
	}
	if s.Status != StatusInitializing {
		t.Errorf("Status = %q", s.Status)
	}
	if s.Region != "us-east" {
		t.Errorf("Region = %q", s.Region)
	}
}

func TestLinodeGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/linode/instances/201" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":      201,
			"label":   "test-vps",
			"status":  "running",
			"type":    "g6-standard-2",
			"region":  "us-east",
			"ipv4":    []string{"1.2.3.4"},
			"ipv6":    "",
			"created": "2026-05-28T10:00:00",
		})
	}))
	defer srv.Close()

	l := NewLinode("test-token")
	l.spec = &ProviderSpec{
		BaseURL:   srv.URL,
		StatusMap: map[string]string{"running": "running"},
	}

	s, err := l.Get(context.Background(), "201")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusRunning {
		t.Errorf("Status = %q", s.Status)
	}
	if s.Plan != "g6-standard-2" {
		t.Errorf("Plan = %q", s.Plan)
	}
}

func TestLinodeDestroy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/linode/instances/201" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	l := NewLinode("test-token")
	l.spec = &ProviderSpec{BaseURL: srv.URL}

	if err := l.Destroy(context.Background(), "201"); err != nil {
		t.Fatal(err)
	}
}

func TestLinodeErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"errors":[{"reason":"Not found"}]}`))
	}))
	defer srv.Close()

	l := NewLinode("test-token")
	l.spec = &ProviderSpec{BaseURL: srv.URL}

	_, err := l.Get(context.Background(), "999")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLinodeDefaultImage(t *testing.T) {
	var receivedImage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		receivedImage, _ = body["image"].(string)
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"id": 1, "label": "x", "status": "provisioning",
			"type": "g6-nanode-1", "region": "us-east",
			"ipv4": []string{}, "ipv6": "", "created": "",
		})
	}))
	defer srv.Close()

	l := NewLinode("test-token")
	l.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "linode/ubuntu24.04",
		StatusMap:    map[string]string{"provisioning": "initializing"},
	}

	l.Create(context.Background(), CreateOpts{Name: "x", Plan: "g6-nanode-1", Region: "us-east"})
	if receivedImage != "linode/ubuntu24.04" {
		t.Errorf("default image = %q, want linode/ubuntu24.04", receivedImage)
	}
}

func TestLinodeUserDataBase64Encoded(t *testing.T) {
	const userData = "#cloud-config\nhostname: test\n"
	var receivedMetadata map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if m, ok := body["metadata"].(map[string]any); ok {
			receivedMetadata = m
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"id": 1, "label": "x", "status": "provisioning",
			"type": "g6-nanode-1", "region": "us-east",
			"ipv4": []string{}, "ipv6": "", "created": "",
		})
	}))
	defer srv.Close()

	l := NewLinode("test-token")
	l.spec = &ProviderSpec{
		BaseURL:      srv.URL,
		DefaultImage: "linode/ubuntu24.04",
		StatusMap:    map[string]string{"provisioning": "initializing"},
	}

	l.Create(context.Background(), CreateOpts{
		Name:     "x",
		Plan:     "g6-nanode-1",
		Region:   "us-east",
		UserData: userData,
	})
	if receivedMetadata == nil {
		t.Fatal("metadata not sent")
	}
	encoded, _ := receivedMetadata["user_data"].(string)
	if encoded == "" {
		t.Fatal("metadata.user_data is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("metadata.user_data is not valid base64: %v", err)
	}
	if string(decoded) != userData {
		t.Errorf("decoded user_data = %q, want %q", decoded, userData)
	}
}
