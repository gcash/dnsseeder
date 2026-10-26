package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gcash/bchd/wire"
)

// TestNetworkConfigs loads every network config in the configs directory and
// checks that each seeder returns only nodes with the services its clients
// ask for, and that the compact filter seeders return every bchd 0.22
// release, which all follow the May 2026 upgrade, and no other node software.
func TestNetworkConfigs(t *testing.T) {
	files, err := filepath.Glob("configs/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no network configs found")
	}
	for _, file := range files {
		s, err := loadNetwork(file)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}

		// Clients, such as bchd, ask a seed's name for full nodes, and
		// x<flags>.<name> for nodes with the services in the hexadecimal
		// flags.
		want := wire.SFNodeNetwork
		if label, _, _ := strings.Cut(s.dnsHost, "."); strings.HasPrefix(label, "x") {
			if flags, err := strconv.ParseUint(label[1:], 16, 64); err == nil {
				want = wire.ServiceFlag(flags)
			}
		}
		var got wire.ServiceFlag
		for _, f := range s.serviceFilter {
			got |= f
		}
		if got&want != want {
			t.Errorf("%s: %s returns nodes without %v", file, s.dnsHost, want&^got)
		}

		if !strings.HasSuffix(file, "-node-cf.json") {
			continue
		}
		for _, ua := range []string{"/bchd:0.22.3(EB32.0)/", "/bchd:0.22.4(EB32.0)/"} {
			if !s.matchesUserAgentFilter(ua) {
				t.Errorf("%s: filters out %s", file, ua)
			}
		}
		for _, ua := range []string{"/bchd:0.21.1(EB32.0)/", "/Bitcoin Cash Node:28.0.1(EB32.0)/"} {
			if s.matchesUserAgentFilter(ua) {
				t.Errorf("%s: returns %s", file, ua)
			}
		}
	}
}
