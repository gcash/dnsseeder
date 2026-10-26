package main

import (
	"errors"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/gcash/bchd/chaincfg"
	"github.com/gcash/bchd/peer"
	"github.com/gcash/bchd/wire"
)

type crawlError struct {
	errLoc string
	Err    error
}

// Error returns a formatted error about a crawl
func (e *crawlError) Error() string {
	return "err: " + e.errLoc + ": " + e.Err.Error()
}

// crawlNode runs in a goroutine, crawls the remote ip and updates the master
// list of currently active addresses
func crawlNode(rc chan *result, s *dnsseeder, nd *node) {

	res := &result{
		node: net.JoinHostPort(nd.NA.IP.String(), strconv.Itoa(int(nd.NA.Port))),
	}

	// connect to the remote ip and ask them for their addr list
	res.nas, res.msg = crawlIP(s, res)

	// all done so push the result back to the seeder.
	//This will block until the seeder reads the result
	rc <- res

	// goroutine will end and be cleaned up
}

// crawlIP retrievs a slice of ip addresses from a client
func crawlIP(s *dnsseeder, r *result) ([]*wire.NetAddress, *crawlError) {
	// The listeners run on the peer's goroutine and must not block it: a
	// message that arrives once the crawl no longer waits for it is dropped.
	verack := make(chan struct{}, 1)
	onAddr := make(chan *wire.MsgAddr, 16)
	peerCfg := &peer.Config{
		UserAgentName:    "dnsseeder", // User agent name to advertise.
		UserAgentVersion: "1.5.3",     // User agent version to advertise.
		Services:         0,
		Listeners: peer.MessageListeners{
			OnAddr: func(_ *peer.Peer, msg *wire.MsgAddr) {
				select {
				case onAddr <- msg:
				default:
				}
			},
			OnVersion: func(_ *peer.Peer, msg *wire.MsgVersion) *wire.MsgReject {
				if config.debug {
					log.Printf("%s - debug - %s - Remote version: %v\n", s.name, r.node, msg.ProtocolVersion)
				}
				// fill the node struct with the remote details
				r.version = msg.ProtocolVersion
				r.services = msg.Services
				r.lastBlock = msg.LastBlock
				r.strVersion = msg.UserAgent
				return nil
			},
			OnVerAck: func(_ *peer.Peer, _ *wire.MsgVerAck) {
				select {
				case verack <- struct{}{}:
				default:
				}
			},
		},
	}
	switch s.id {
	case 0xf4f3e5f4:
		peerCfg.ChainParams = &chaincfg.TestNet3Params
	case 0xafdab7e2:
		if s.port == 48333 {
			peerCfg.ChainParams = &chaincfg.ChipNetParams
		} else {
			peerCfg.ChainParams = &chaincfg.TestNet4Params
		}
	default:
		peerCfg.ChainParams = &chaincfg.MainNetParams
	}
	p, err := peer.NewOutboundPeer(peerCfg, r.node)
	if err != nil {
		return nil, &crawlError{"NewOutboundPeer: error", err}
	}

	// Establish the connection to the peer address and mark it connected.
	// Give up after 5 seconds, BCHN's default connect timeout, instead of
	// when the system does, after about two minutes on Linux.
	conn, err := net.DialTimeout("tcp", p.Addr(), time.Second*5)
	if err != nil {
		return nil, &crawlError{"net.Dial: error", err}
	}
	p.AssociateConnection(conn)
	defer p.WaitForDisconnect()
	defer p.Disconnect()

	// Wait for the verack message or timeout in case of failure.
	select {
	case <-verack:
	case <-time.After(time.Second * 5):
		return nil, &crawlError{"Verack timeout", errors.New("")}
	}

	// if we get this far and if the seeder is full then don't ask for addresses. This will reduce bandwidth usage while still
	// confirming that we can connect to the remote node
	s.mtx.RLock()
	full := len(s.theList) > s.maxSize
	s.mtx.RUnlock()
	if full {
		return nil, nil
	}

	// send getaddr command
	p.QueueMessage(wire.NewMsgGetAddr(), nil)

	// BCHN sends its own address right after the handshake, and its answer
	// to getaddr, up to 1000 addresses, on a later address broadcast: up
	// to 46 seconds later in a probe of BCHN nodes, or not at all.  So
	// collect the addresses of every addr message until one with more than
	// one address arrives, for up to a minute.
	var addrs []*wire.NetAddress
	timeout := time.After(time.Minute)
	for {
		select {
		case msg := <-onAddr:
			addrs = append(addrs, msg.AddrList...)
			if len(msg.AddrList) > 1 {
				return addrs, nil
			}
		case <-timeout:
			return addrs, nil
		}
	}
}
