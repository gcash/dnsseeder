package main

import (
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gcash/bchd/wire"
)

// TestNetworkConfigs loads every network config and template in the configs
// directory and checks that each of its names returns only nodes with the
// services its clients ask for, and that the names with a user agent filter,
// the compact filter ones, return every bchd 0.22 release, which all follow
// the May 2026 upgrade, and no other node software.
func TestNetworkConfigs(t *testing.T) {
	files, err := filepath.Glob("configs/*.json")
	if err != nil {
		t.Fatal(err)
	}
	templates, err := filepath.Glob("configs/*.json.template")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, templates...)
	if len(files) == 0 {
		t.Fatal("no network configs found")
	}
	for _, file := range files {
		s, err := loadNetwork(file)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}

		for _, n := range s.names {
			// Clients, such as bchd, ask a seed's name for full nodes,
			// and x<flags>.<name> for nodes with the services in the
			// hexadecimal flags.
			want := wire.SFNodeNetwork
			if label, _, _ := strings.Cut(n.host, "."); strings.HasPrefix(label, "x") {
				if flags, err := strconv.ParseUint(label[1:], 16, 64); err == nil {
					want = wire.ServiceFlag(flags)
				}
			}
			var got wire.ServiceFlag
			for _, f := range n.serviceFilter {
				got |= f
			}
			if got&want != want {
				t.Errorf("%s: %s returns nodes without %v", file, n.host, want&^got)
			}

			if len(n.userAgentFilter) == 0 {
				continue
			}
			for _, ua := range []string{"/bchd:0.22.3(EB32.0)/", "/bchd:0.22.4(EB32.0)/"} {
				if !n.matchesUserAgentFilter(ua) {
					t.Errorf("%s: %s filters out %s", file, n.host, ua)
				}
			}
			for _, ua := range []string{"/bchd:0.21.1(EB32.0)/", "/Bitcoin Cash Node:28.0.1(EB32.0)/"} {
				if n.matchesUserAgentFilter(ua) {
					t.Errorf("%s: %s returns %s", file, n.host, ua)
				}
			}
		}
	}
}

// TestDNSNamesConfig ensures a network config can serve several dns names,
// each with its own filters, that the one name format still loads, and that a
// config with both, with a name twice, with a name another config serves, with
// an empty name, or with a service it doesn't know, is refused.
func TestDNSNamesConfig(t *testing.T) {
	config.dataDir = t.TempDir()
	config.seeders = make(map[string]*dnsseeder)
	base := JNetwork{Name: "Names Test", ID: "0xe8f3e1e3", Port: 8333,
		NameServer: "ns.example.org"}

	multi := base
	multi.DNSNames = []JDNSName{
		{Name: "Seed.Example.org", ServiceFilter: []string{"NodeNetwork"}},
		{Name: "x101.seed.example.org", ServiceFilter: []string{"NodeNetwork", "NodeCF"},
			UserAgentFilter: []string{"bchd:0.22."}},
	}
	s, err := initNetwork(multi)
	if err != nil {
		t.Fatal(err)
	}
	want := []dnsName{
		{host: "seed.example.org", serviceFilter: []wire.ServiceFlag{wire.SFNodeNetwork}},
		{host: "x101.seed.example.org",
			serviceFilter:   []wire.ServiceFlag{wire.SFNodeNetwork, wire.SFNodeCF},
			userAgentFilter: []string{"bchd:0.22."}},
	}
	if !reflect.DeepEqual(s.names, want) {
		t.Errorf("names %+v, want %+v", s.names, want)
	}

	one := base
	one.DNSName = "One.Example.org"
	one.ServiceFilter = []string{"NodeNetwork"}
	if s, err := initNetwork(one); err != nil {
		t.Error(err)
	} else if want := []dnsName{{host: "one.example.org",
		serviceFilter: []wire.ServiceFlag{wire.SFNodeNetwork}}}; !reflect.DeepEqual(s.names, want) {

		t.Errorf("one name format: names %+v, want %+v", s.names, want)
	}

	config.seeders[s.name] = s
	both := base
	both.Name, both.DNSName = "Both Test", "both.example.org"
	both.DNSNames = []JDNSName{{Name: "both2.example.org"}}
	twice := base
	twice.Name = "Twice Test"
	twice.DNSNames = []JDNSName{{Name: "twice.example.org"}, {Name: "TWICE.example.org"}}
	taken := base
	taken.Name = "Taken Test"
	taken.DNSNames = []JDNSName{{Name: "taken.example.org"}, {Name: "x101.seed.example.org"}}
	empty := base
	empty.Name = "Empty Test"
	empty.DNSNames = []JDNSName{{Name: "empty.example.org"}, {Name: ""}}
	misspelt := base
	misspelt.Name = "Misspelt Test"
	misspelt.DNSNames = []JDNSName{{Name: "misspelt.example.org",
		ServiceFilter: []string{"NodeNetwork", "NodeCompactFilters"}}}
	misspeltOne := base
	misspeltOne.Name = "Misspelt One Test"
	misspeltOne.DNSName = "misspelt-one.example.org"
	misspeltOne.ServiceFilter = []string{"NodeNetwrok"}

	refused := []struct {
		jnw    JNetwork
		reason string
	}{
		{both, "DNSNames is set with DNSName"},
		{twice, "duplicate DNS name twice.example.org in Twice Test"},
		{taken, "Already loaded x101.seed.example.org for Names Test"},
		{empty, "no DNS hostname supplied"},
		{misspelt, `misspelt.example.org: unknown service "NodeCompactFilters"`},
		{misspeltOne, `misspelt-one.example.org: unknown service "NodeNetwrok"`},
	}
	for _, test := range refused {
		_, err := initNetwork(test.jnw)
		if err == nil || !strings.Contains(err.Error(), test.reason) {
			t.Errorf("%s: %v, want an error with %q", test.jnw.Name, err, test.reason)
		}
	}
}
