package warp

import (
	"testing"
)

func TestWarpPrefixes(t *testing.T) {
	prefixes := WarpPrefixes()
	if len(prefixes) == 0 {
		t.Fatal("WarpPrefixes returned empty list")
	}
	for _, p := range prefixes {
		if !p.IsValid() {
			t.Errorf("Invalid prefix: %v", p)
		}
	}
}

func TestRandomWarpPrefix(t *testing.T) {
	// Test IPv4
	p4 := RandomWarpPrefix(true, false)
	if !p4.Addr().Is4() {
		t.Errorf("Expected IPv4 prefix, got %v", p4)
	}

	// Test IPv6
	p6 := RandomWarpPrefix(false, true)
	if !p6.Addr().Is6() {
		t.Errorf("Expected IPv6 prefix, got %v", p6)
	}

	// Test Mixed (should return either, but valid)
	p := RandomWarpPrefix(true, true)
	if !p.IsValid() {
		t.Errorf("Invalid prefix: %v", p)
	}
}

func TestRandomWarpEndpoint(t *testing.T) {
	ep, err := RandomWarpEndpoint(true, false)
	if err != nil {
		t.Fatalf("RandomWarpEndpoint failed: %v", err)
	}
	if !ep.Addr().Is4() {
		t.Errorf("Expected IPv4 endpoint, got %v", ep)
	}
	if ep.Port() == 0 {
		t.Error("Endpoint port is 0")
	}
}

func TestWarpPorts(t *testing.T) {
	ports := WarpPorts()
	if len(ports) == 0 {
		t.Fatal("WarpPorts returned empty list")
	}
}
