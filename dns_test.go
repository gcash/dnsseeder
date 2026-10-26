package main

import (
	"net"
	"testing"
	"time"

	"github.com/gcash/bchd/wire"
	"github.com/miekg/dns"
)

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

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	server := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(handleDNS),
		NotifyStartedFunc: func() { close(started) }}
	go func() { _ = server.ActivateAndServe() }()
	<-started
	defer func() { _ = server.Shutdown() }()

	names := []string{"case-test.example.org.", "CASE-TEST.EXAMPLE.ORG.",
		"cAsE-tEsT.eXaMpLe.OrG."}
	qtypes := []uint16{dns.TypeA, dns.TypeNS, dns.TypeSOA}
	for _, name := range names {
		for _, qtype := range qtypes {
			msg := name + " " + dns.TypeToString[qtype]
			q := new(dns.Msg)
			q.SetQuestion(name, qtype)
			reply, err := dns.Exchange(q, pc.LocalAddr().String())
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

	// The requests are counted in goroutines.
	want := uint32(len(names) * len(qtypes))
	deadline := time.Now().Add(5 * time.Second)
	for {
		var got uint32
		s.counts.mtx.RLock()
		for _, n := range s.counts.DNSCounts {
			got += n
		}
		s.counts.mtx.RUnlock()
		if got == want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d requests counted", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
