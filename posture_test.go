package capstan

import (
	"testing"
)

func TestPostureDataLoads(t *testing.T) {
	if len(posture.GeoRouting) == 0 {
		t.Fatal("GeoRouting is empty")
	}
	if len(posture.TierMap) == 0 {
		t.Fatal("TierMap is empty")
	}
}

func TestRecommendPlacement_EUCentral(t *testing.T) {
	r, err := RecommendPlacement(GeoEUCentral, WorkloadIOMultitenant, SLAStandard)
	if err != nil {
		t.Fatal(err)
	}
	if r.Primary.Provider != Hetzner {
		t.Errorf("primary provider = %q, want hetzner", r.Primary.Provider)
	}
	if r.Primary.Region != "fsn1" {
		t.Errorf("primary region = %q, want fsn1", r.Primary.Region)
	}
	if r.Primary.Size != "cx43" {
		t.Errorf("primary size = %q, want cx43", r.Primary.Size)
	}
	if len(r.Fallbacks) < 1 {
		t.Error("expected fallbacks")
	}
}

func TestRecommendPlacement_USEast(t *testing.T) {
	r, err := RecommendPlacement(GeoUSEast, WorkloadIOMultitenant, SLAPremium)
	if err != nil {
		t.Fatal(err)
	}
	if r.Primary.Provider != Hetzner {
		t.Errorf("primary provider = %q, want hetzner", r.Primary.Provider)
	}
	if r.Primary.Region != "ash" {
		t.Errorf("primary region = %q, want ash", r.Primary.Region)
	}
	if r.Primary.Size != "ccx33" {
		t.Errorf("primary size = %q, want ccx33", r.Primary.Size)
	}
	if len(r.Caveats) == 0 {
		t.Error("expected caveats for hetzner/ash")
	}
}

func TestRecommendPlacement_Australia(t *testing.T) {
	r, err := RecommendPlacement(GeoAustralia, WorkloadIOMultitenant, SLAStandard)
	if err != nil {
		t.Fatal(err)
	}
	if r.Primary.Provider != Linode {
		t.Errorf("primary provider = %q, want linode", r.Primary.Provider)
	}
	if r.Primary.Region != "ap-southeast" {
		t.Errorf("primary region = %q, want ap-southeast", r.Primary.Region)
	}
	if len(r.Caveats) == 0 {
		t.Error("expected Sydney caveat")
	}
}

func TestRecommendPlacement_Defaults(t *testing.T) {
	r, err := RecommendPlacement(GeoJapan, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Primary.Size != "g6-standard-4" {
		t.Errorf("default workload+sla should give g6-standard-4, got %q", r.Primary.Size)
	}
}

func TestRecommendPlacement_InvalidGeo(t *testing.T) {
	_, err := RecommendPlacement("antarctica", "", "")
	if err == nil {
		t.Error("expected error for unknown geography")
	}
}

func TestRecommendPlacement_AllGeographies(t *testing.T) {
	geos := []Geography{
		GeoEUCentral, GeoEUNorth, GeoEUWest, GeoEUSouth,
		GeoUSEast, GeoUSCentral, GeoUSWest, GeoCanada,
		GeoSingapore, GeoJapan, GeoIndia, GeoIndonesia,
		GeoAustralia, GeoLatam,
	}
	for _, geo := range geos {
		r, err := RecommendPlacement(geo, WorkloadGeneral, SLAStandard)
		if err != nil {
			t.Errorf("RecommendPlacement(%q) failed: %v", geo, err)
			continue
		}
		if r.Primary.Provider == "" || r.Primary.Region == "" || r.Primary.Size == "" {
			t.Errorf("RecommendPlacement(%q) returned incomplete placement: %+v", geo, r.Primary)
		}
	}
}
