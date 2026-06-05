package capstan

import (
	"testing"
)

func TestAllSpecsLoad(t *testing.T) {
	for _, name := range AllProviders() {
		s := Spec(name)
		if s == nil {
			t.Fatalf("Spec(%q) returned nil", name)
		}
		if s.Name != string(name) {
			t.Errorf("Spec(%q).Name = %q", name, s.Name)
		}
		if s.BaseURL == "" {
			t.Errorf("Spec(%q).BaseURL is empty", name)
		}
		if s.DefaultImage == "" {
			t.Errorf("Spec(%q).DefaultImage is empty", name)
		}
		if len(s.StatusMap) == 0 {
			t.Errorf("Spec(%q).StatusMap is empty", name)
		}
		if len(s.PriceCents) == 0 {
			t.Errorf("Spec(%q).PriceCents is empty", name)
		}
	}
}

func TestHetznerSpec(t *testing.T) {
	s := Spec(Hetzner)
	if s.BaseURL != "https://api.hetzner.cloud/v1" {
		t.Errorf("baseUrl = %q", s.BaseURL)
	}
	if s.DefaultImage != "ubuntu-24.04" {
		t.Errorf("defaultImage = %q", s.DefaultImage)
	}
	if s.UserDataEncoding != "raw" {
		t.Errorf("userDataEncoding = %q", s.UserDataEncoding)
	}
	if s.MapStatus("running") != StatusRunning {
		t.Errorf("MapStatus(running) = %q", s.MapStatus("running"))
	}
	if s.MapStatus("initializing") != StatusInitializing {
		t.Errorf("MapStatus(initializing) = %q", s.MapStatus("initializing"))
	}
	if s.MapStatus("off") != StatusStopped {
		t.Errorf("MapStatus(off) = %q", s.MapStatus("off"))
	}
	if s.MapStatus("migrating") != StatusUnknown {
		t.Errorf("MapStatus(migrating) should be unknown, got %q", s.MapStatus("migrating"))
	}
	if c := s.EstimateMonthlyCost("cx23"); c <= 0 {
		t.Errorf("cost(cx23) = %d, want > 0", c)
	}
}

func TestDigitalOceanSpec(t *testing.T) {
	s := Spec(DigitalOcean)
	if s.BaseURL != "https://api.digitalocean.com/v2" {
		t.Errorf("baseUrl = %q", s.BaseURL)
	}
	if s.DefaultImage != "ubuntu-24-04-x64" {
		t.Errorf("defaultImage = %q", s.DefaultImage)
	}
	if s.MapStatus("new") != StatusInitializing {
		t.Errorf("MapStatus(new) = %q", s.MapStatus("new"))
	}
	if s.MapStatus("active") != StatusRunning {
		t.Errorf("MapStatus(active) = %q", s.MapStatus("active"))
	}
	if c := s.EstimateMonthlyCost("s-1vcpu-1gb"); c <= 0 {
		t.Errorf("cost(s-1vcpu-1gb) = %d, want > 0", c)
	}
}

func TestLinodeSpec(t *testing.T) {
	s := Spec(Linode)
	if s.BaseURL != "https://api.linode.com/v4" {
		t.Errorf("baseUrl = %q", s.BaseURL)
	}
	if s.UserDataEncoding != "base64" {
		t.Errorf("userDataEncoding = %q", s.UserDataEncoding)
	}
	if s.MapStatus("provisioning") != StatusInitializing {
		t.Errorf("MapStatus(provisioning) = %q", s.MapStatus("provisioning"))
	}
	if s.MapStatus("running") != StatusRunning {
		t.Errorf("MapStatus(running) = %q", s.MapStatus("running"))
	}
	if c := s.EstimateMonthlyCost("g6-nanode-1"); c <= 0 {
		t.Errorf("cost(g6-nanode-1) = %d, want > 0", c)
	}
}

func TestVultrSpec(t *testing.T) {
	s := Spec(Vultr)
	if s.BaseURL != "https://api.vultr.com/v2" {
		t.Errorf("baseUrl = %q", s.BaseURL)
	}
	if s.UserDataEncoding != "base64" {
		t.Errorf("userDataEncoding = %q", s.UserDataEncoding)
	}
	if s.MapStatus("pending") != StatusInitializing {
		t.Errorf("MapStatus(pending) = %q", s.MapStatus("pending"))
	}
	if s.MapStatus("active/running") != StatusRunning {
		t.Errorf("MapStatus(active/running) = %q", s.MapStatus("active/running"))
	}
	if s.MapStatus("active/stopped") != StatusStopped {
		t.Errorf("MapStatus(active/stopped) = %q", s.MapStatus("active/stopped"))
	}
	if c := s.EstimateMonthlyCost("vc2-1c-1gb"); c <= 0 {
		t.Errorf("cost(vc2-1c-1gb) = %d, want > 0", c)
	}
}

func TestResolveImage(t *testing.T) {
	s := Spec(Hetzner)
	if img := s.ResolveImage(""); img != "ubuntu-24.04" {
		t.Errorf("ResolveImage('') = %q", img)
	}
	if img := s.ResolveImage("debian-12"); img != "debian-12" {
		t.Errorf("ResolveImage('debian-12') = %q", img)
	}
}
