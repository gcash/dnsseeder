package main

import (
	"math"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gcash/bchd/wire"
	"github.com/miekg/dns"
)

// startDNSServer serves handleDNS on a loopback UDP port until the test ends,
// and returns its address.
func startDNSServer(t *testing.T) string {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	server := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(handleDNS),
		NotifyStartedFunc: func() { close(started) }}
	go func() { _ = server.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = server.Shutdown() })
	return pc.LocalAddr().String()
}

// TestHandleDNSCase ensures a seeder answers, and counts, a request whatever
// the case of the name asked for, as resolvers such as Google Public DNS
// randomize it (DNS 0x20), also when its config names it in mixed case.
func TestHandleDNSCase(t *testing.T) {
	config.dataDir = t.TempDir()
	config.seeders = make(map[string]*dnsseeder)
	config.dns = make(map[string][]dns.RR)

	s, err := initNetwork(JNetwork{
		Name:       "Case Test",
		ID:         "0xe3e1f3e8",
		Port:       8333,
		DNSName:    "Case-Test.Example.ORG",
		NameServer: "ns.example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	config.seeders[s.name] = s
	na := wire.NewNetAddress(&net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 8333}, 0)
	if !s.addNa(na) {
		t.Fatal("node not added")
	}
	for _, nd := range s.theList {
		nd.Status = statusCG
	}
	updateDNS(s)
	addr := startDNSServer(t)
	unknown := atomic.LoadUint64(&config.dnsUnknown)

	names := []string{"case-test.example.org.", "CASE-TEST.EXAMPLE.ORG.",
		"cAsE-tEsT.eXaMpLe.OrG."}
	qtypes := []uint16{dns.TypeA, dns.TypeNS, dns.TypeSOA}
	for _, name := range names {
		for _, qtype := range qtypes {
			msg := name + " " + dns.TypeToString[qtype]
			q := new(dns.Msg)
			q.SetQuestion(name, qtype)
			reply, err := dns.Exchange(q, addr)
			if err != nil {
				t.Fatalf("%s: %v", msg, err)
			}
			if len(reply.Answer) != 1 {
				t.Errorf("%s: %d answers", msg, len(reply.Answer))
			}
			// A resolver checks that the reply asks for the name
			// in the case it sent.
			if len(reply.Question) != 1 || reply.Question[0].Name != name {
				t.Errorf("%s: reply asks for %v", msg, reply.Question)
			}
		}
	}

	waitCounted(t, s, uint32(len(names)*len(qtypes)), unknown, 0)
}

// waitCounted waits until the server has counted n requests for the seeder s,
// and unknown more for no seeder than unknownBefore.  It counts each request
// in a goroutine that reads the seeders, which the next test replaces.
func waitCounted(t *testing.T, s *dnsseeder, n uint32, unknownBefore, unknown uint64) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var got uint32
		s.counts.mtx.RLock()
		for _, c := range s.counts.DNSCounts {
			got += c
		}
		s.counts.mtx.RUnlock()
		gotUnknown := atomic.LoadUint64(&config.dnsUnknown) - unknownBefore
		if got == n && gotUnknown == unknown {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d requests counted for the seeder, %d of %d "+
				"for none", got, n, gotUnknown, unknown)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestUpdateDNSHeights ensures a seeder returns only the good nodes whose
// height is within maxHeightDiff of the median height of its good nodes, and
// leaves out nodes of other chains and nodes that stopped syncing, which it
// returned too.
func TestUpdateDNSHeights(t *testing.T) {
	config.dns = make(map[string][]dns.RR)
	s := &dnsseeder{dnsHost: "height-test.example.org", ttl: 60, port: 8333,
		maxSize: 100, theList: make(map[string]*node)}

	nodes := []struct {
		ip     string
		height int32
		status uint32
		served bool
	}{
		{"10.0.0.1", 1000, statusCG, true},
		{"10.0.0.2", 1000, statusCG, true},
		{"10.0.0.3", 1001, statusCG, true},
		{"10.0.0.4", 999, statusCG, true},
		{"10.0.0.5", 1000 - maxHeightDiff, statusCG, true},
		{"10.0.0.6", 1000 - maxHeightDiff - 1, statusCG, false},
		{"10.0.0.7", 1000 + maxHeightDiff + 1, statusCG, false},
		{"10.0.0.8", math.MaxInt32, statusCG, false},
		// Not a good node, so its height doesn't count toward the median.
		{"10.0.0.9", 5, statusWG, false},
	}
	for _, n := range nodes {
		if !s.addNa(wire.NewNetAddressIPPort(net.ParseIP(n.ip), 8333, 0)) {
			t.Fatalf("%s not added", n.ip)
		}
		nd := s.theList[net.JoinHostPort(n.ip, "8333")]
		nd.Status, nd.LastBlock = n.status, n.height
	}
	updateDNS(s)

	served := make(map[string]bool)
	for _, rr := range config.dns["height-test.example.org.A"] {
		served[rr.(*dns.A).A.String()] = true
	}
	for _, n := range nodes {
		if served[n.ip] != n.served {
			t.Errorf("%s at height %d: served %v, want %v", n.ip, n.height,
				served[n.ip], n.served)
		}
	}
}

// testSeeder makes a mainnet seeder for the DNS name name the server's only
// one, with a good node at height 1000 for each address in ips, and builds its
// records.
func testSeeder(t *testing.T, name string, ips ...net.IP) *dnsseeder {
	t.Helper()

	config.dataDir = t.TempDir()
	config.seeders = make(map[string]*dnsseeder)
	config.dns = make(map[string][]dns.RR)
	s, err := initNetwork(JNetwork{
		Name:       name,
		ID:         "0xe8f3e1e3",
		Port:       8333,
		DNSName:    name,
		NameServer: "ns.example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	config.seeders[s.name] = s
	for _, ip := range ips {
		if !s.addNa(wire.NewNetAddressIPPort(ip, 8333, 0)) {
			t.Fatalf("%s not added", ip)
		}
	}
	for _, nd := range s.theList {
		nd.Status, nd.LastBlock = statusCG, 1000
	}
	updateDNS(s)
	return s
}

// TestHandleDNSSize ensures an answer of 25 addresses fits the 512 bytes of a
// UDP reply without EDNS.  Uncompressed it took 1,068.
func TestHandleDNSSize(t *testing.T) {
	var ips []net.IP
	for i := 1; i <= 30; i++ {
		ips = append(ips, net.IPv4(10, 0, 0, byte(i)))
	}
	s := testSeeder(t, "compress-test.example.org", ips...)
	addr := startDNSServer(t)
	unknown := atomic.LoadUint64(&config.dnsUnknown)

	q := new(dns.Msg)
	q.SetQuestion("compress-test.example.org.", dns.TypeA)
	query, err := q.Pack()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(query); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}

	reply := new(dns.Msg)
	if err := reply.Unpack(buf[:n]); err != nil {
		t.Fatal(err)
	}
	if len(reply.Answer) != 25 {
		t.Errorf("%d addresses in the answer", len(reply.Answer))
	}
	if n > 512 {
		t.Errorf("answer of %d bytes", n)
	}
	t.Logf("answer of %d addresses in %d bytes", len(reply.Answer), n)
	waitCounted(t, s, 1, unknown, 0)
}

// TestHandleDNSZones ensures the server answers for the names of its seeders
// and the names under them, and refuses other names, which it answered as if
// it had authority for them.
func TestHandleDNSZones(t *testing.T) {
	s := testSeeder(t, "zone-test.example.org", net.IPv4(10, 0, 0, 1))
	addr := startDNSServer(t)
	unknown := atomic.LoadUint64(&config.dnsUnknown)

	tests := []struct {
		name    string
		rcode   int
		answers int
	}{
		{"zone-test.example.org.", dns.RcodeSuccess, 1},
		{"ZONE-test.example.org.", dns.RcodeSuccess, 1},
		{"nonstd.zone-test.example.org.", dns.RcodeSuccess, 0},
		// The seeder counts the requests above as its own, and the rest
		// as unknown.
		{"x5.zone-test.example.org.", dns.RcodeSuccess, 0},
		{"example.org.", dns.RcodeRefused, 0},
		{"other-zone-test.example.org.", dns.RcodeRefused, 0},
		{"google.com.", dns.RcodeRefused, 0},
	}
	for _, test := range tests {
		q := new(dns.Msg)
		q.SetQuestion(test.name, dns.TypeA)
		reply, err := dns.Exchange(q, addr)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if reply.Rcode != test.rcode || len(reply.Answer) != test.answers {
			t.Errorf("%s: %s with %d answers, want %s with %d", test.name,
				dns.RcodeToString[reply.Rcode], len(reply.Answer),
				dns.RcodeToString[test.rcode], test.answers)
		}
		if reply.Authoritative != (test.rcode == dns.RcodeSuccess) {
			t.Errorf("%s: authoritative %v", test.name, reply.Authoritative)
		}
	}
	waitCounted(t, s, 3, unknown, 4)
}
