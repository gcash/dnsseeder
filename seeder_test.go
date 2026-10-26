package main

import (
	"net"
	"strconv"
	"testing"

	"github.com/gcash/bchd/wire"
)

func TestGetNonStdIP(t *testing.T) {

	var iptests = []struct {
		rip   string
		port  uint16
		encip string
	}{
		{"1.2.3.4", 1234, "137.195.4.210"},
		{"50.123.45.67", 43210, "101.165.168.202"},
		{"202.36.170.3", 65535, "199.31.255.255"},
		{"123.213.132.231", 34, "12.91.0.34"},
	}

	for _, atest := range iptests {
		newip := getNonStdIP(net.ParseIP(atest.rip), atest.port)
		if newip.String() != atest.encip {
			t.Errorf("real-ip: %s real-port: %v encoded-ip: %v expected-ip: %s", atest.rip, atest.port, newip, atest.encip)
		}
	}
}

func TestAddnNa(t *testing.T) {
	// create test data struct
	var td = []struct {
		ip      string
		port    int
		dnsType uint32
	}{
		{"1.2.3.4", 28333, 1},
		{"50.123.45.67", 43210, 2},
	}

	s := &dnsseeder{
		port:    28333,
		pver:    1234,
		maxSize: 1,
	}
	s.theList = make(map[string]*node)

	for _, atest := range td {
		// Test NewNetAddress.
		tcpAddr := &net.TCPAddr{
			IP:   net.ParseIP(atest.ip),
			Port: atest.port,
		}
		na := wire.NewNetAddress(tcpAddr, 0)
		ndName := net.JoinHostPort(na.IP.String(), strconv.Itoa(int(na.Port)))

		result := s.addNa(na)
		if !result {
			t.Errorf("failed to create new node: %s", ndName)
		}
		if s.theList[ndName].DNSType != atest.dnsType {
			t.Errorf("node: %s dnsType:%v expected: %v", ndName, s.theList[ndName].DNSType, atest.dnsType)
		}
	}

	tcpAddr := &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 1234,
	}
	na := wire.NewNetAddress(tcpAddr, 0)
	result := s.addNa(na)

	if result {
		t.Errorf("node added but should have failed as seeder full: %s", net.JoinHostPort(na.IP.String(), strconv.Itoa(int(na.Port))))
	}

	tcpAddr = &net.TCPAddr{
		IP:   net.ParseIP("1.2.3.4"),
		Port: 28333,
	}
	na = wire.NewNetAddress(tcpAddr, 0)
	result = s.addNa(na)

	if result {
		t.Errorf("node added but should have failed as duplicate: %s", net.JoinHostPort(na.IP.String(), strconv.Itoa(int(na.Port))))
	}

}

// TestProcessResultVersion ensures a seeder returns only nodes that report at
// least its minimum protocol version, Pver, which it ignored.
func TestProcessResultVersion(t *testing.T) {
	s := &dnsseeder{port: 8333, pver: 70016, maxSize: 100}
	s.theList = make(map[string]*node)
	if !s.addNa(wire.NewNetAddressIPPort(net.IPv4(1, 2, 3, 4), 8333, 0)) {
		t.Fatal("node not added")
	}
	key := net.JoinHostPort("1.2.3.4", "8333")

	tests := []struct {
		version int32
		status  uint32
	}{
		{70016, statusCG},
		{80003, statusCG},
		{70015, statusWG},
		{-1, statusWG},
	}
	for _, test := range tests {
		s.processResult(&result{node: key, version: test.version})
		if got := s.theList[key].Status; got != test.status {
			t.Errorf("version %d: %s, want %s", test.version,
				status2str(got), status2str(test.status))
		}
	}
}

// TestAuditKeepsFilteredNodes ensures the audit, which cycles out the good
// nodes beyond its goal, keeps the ones a filtered name returns, which are few
// among the good nodes of a network, so they don't drop out of its answers.
func TestAuditKeepsFilteredNodes(t *testing.T) {
	config.dataDir = t.TempDir()
	config.seeders = make(map[string]*dnsseeder)
	s, err := initNetwork(JNetwork{Name: "Audit Test", ID: "0xe8f3e1e3", Port: 8333,
		NameServer: "ns.example.org",
		DNSNames: []JDNSName{
			{Name: "audit-test.example.org", ServiceFilter: []string{"NodeNetwork"}},
			{Name: "x101.audit-test.example.org", ServiceFilter: []string{"NodeNetwork", "NodeCF"}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	// A goal of one good node.
	s.delay[statusCG], s.maxStart[statusCG] = crawlDelay, 2

	for i := 1; i <= 20; i++ {
		if !s.addNa(wire.NewNetAddressIPPort(net.IPv4(10, 0, 0, byte(i)), 8333, 0)) {
			t.Fatalf("node %d not added", i)
		}
	}
	cfNodes := 0
	for _, nd := range s.theList {
		nd.Status, nd.Services = statusCG, wire.SFNodeNetwork
		if cfNodes < 3 {
			nd.Services |= wire.SFNodeCF
			cfNodes++
		}
	}
	s.auditNodes()

	cf, other := 0, 0
	for _, nd := range s.theList {
		if nd.Services&wire.SFNodeCF != 0 {
			cf++
		} else {
			other++
		}
	}
	if cf != 3 {
		t.Errorf("%d of the 3 compact filter nodes kept", cf)
	}
	if other > 1 {
		t.Errorf("%d other good nodes kept, want at most 1", other)
	}
}
