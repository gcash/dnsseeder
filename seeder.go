package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gcash/bchd/wire"
)

const (

	// NOUNCE is used to check if we connect to ourselves
	// as we don't listen we can use a fixed value
	nounce  = 0x0539a019ca550825
	minPort = 0
	maxPort = 65535

	crawlDelay     = 22 // seconds between start crawlwer ticks
	auditDelay     = 22 // minutes between audit channel ticks
	dnsDelay       = 57 // seconds between updates to active dns record list
	cacheDumpDelay = 10 // minutes between writing cache to disk

	maxFails = 58 // max number of connect fails before we delete a node. Just over 24 hours(checked every 33 minutes)
)

const (
	dnsInvalid  = iota //
	dnsV4Std           // ip v4 using network standard port
	dnsV4Non           // ip v4 using network non standard port
	dnsV6Std           // ipv6 using network standard port
	dnsV6Non           // ipv6 using network non standard port
	maxDNSTypes        // used in main to allocate slice
)

const (
	// node status
	statusRG       = iota // reported good status. A remote node has reported this ip but we have not connected
	statusCG              // confirmed good. We have connected to the node and received addresses
	statusWG              // was good. node was confirmed good but now having problems
	statusNG              // no good. Will be removed from theList after 24 hours to redure bouncing ip addresses
	maxStatusTypes        // used in main to allocate slice
)

type dnsseeder struct {
	id         wire.BitcoinNet  // Magic number - Unique ID for this network. Sent in header of all messages
	theList    map[string]*node // the list of current nodes
	mtx        sync.RWMutex     // protect thelist
	names      []dnsName        // the dns names we serve results for, each with its filters
	nameServer string           // the hostname of the nameserver
	mbox       string           // E-Mail address reported in SOA records
	name       string           // Short name for the network
	desc       string           // Long description for the network
	initialIPs []string         // Initial ip address to connect to and ask for addresses if we have no seeders
	seeders    []string         // slice of seeders to pull ip addresses when starting this seeder
	maxStart   []uint32         // max number of goroutines to start each run for each status type
	delay      []int64          // number of seconds to wait before we connect to a known client for each status
	counts     NodeCounts       // structure to hold stats for this seeder
	pver       uint32           // minimum protocol version a node must speak to be returned
	ttl        uint32           // DNS TTL to use for this seeder
	maxSize    int              // max number of clients before we start restricting new entries
	port       uint16           // default network port this seeder uses
}

// dnsName is a dns name a seeder serves, with the filters of the nodes it
// returns.  The seeder crawls the network once for all its names.
type dnsName struct {
	host            string             // the name, in lower case
	serviceFilter   []wire.ServiceFlag // Only respond with nodes that support this filter
	userAgentFilter []string           // Only respond with nodes whose useragent contains one of these strings
}

// returns returns true if the name returns nd: the node advertises every
// service of the name's service filter and passes its user agent filter.
func (n *dnsName) returns(nd *node) bool {
	for _, service := range n.serviceFilter {
		if !HasService(nd.Services, service) {
			return false
		}
	}
	return n.matchesUserAgentFilter(nd.StrVersion)
}

// matchesUserAgentFilter returns true if a node with the user agent ua passes
// the name's user agent filter: its user agent contains one of the filter's
// strings, ignoring case.  Every node passes when there is no filter.
func (n *dnsName) matchesUserAgentFilter(ua string) bool {
	if len(n.userAgentFilter) == 0 {
		return true
	}
	for _, f := range n.userAgentFilter {
		if strings.Contains(strings.ToLower(ua), strings.ToLower(f)) {
			return true
		}
	}
	return false
}

// filtersFullNodes returns true if the name returns only some of the full
// nodes, as its filters ask for more than NodeNetwork.
func (n *dnsName) filtersFullNodes() bool {
	if len(n.userAgentFilter) > 0 {
		return true
	}
	for _, service := range n.serviceFilter {
		if service != wire.SFNodeNetwork {
			return true
		}
	}
	return false
}

type result struct {
	nas        []*wire.NetAddress // slice of node addresses returned from a node
	msg        *crawlError        // error string or nil if no problems
	node       string             // theList key to the node that was crawled
	version    int32              // remote node protocol version
	services   wire.ServiceFlag   // remote client supported services
	lastBlock  int32              // last block seen by the node
	strVersion string             // remote client user agent
}

// initCrawlers needs to be run before the startCrawlers so it can get
// a list of current ip addresses from the other seeders and therefore
// start the crawl process
func (s *dnsseeder) initSeeder() {

	// range over existing seeders for the network and get starting ip addresses from them
	for _, aseeder := range s.seeders {
		c := 0

		if aseeder == "" {
			continue
		}
		newRRs, err := net.LookupHost(aseeder)
		if err != nil {
			log.Printf("%s: unable to do initial lookup to seeder %s %v\n", s.name, aseeder, err)
			continue
		}

		for _, ip := range newRRs {
			if newIP := net.ParseIP(ip); newIP != nil {
				// 1 at the end is the services flag
				if x := s.addNa(wire.NewNetAddressIPPort(newIP, s.port, 1)); x {
					c++
				}
			}
		}
		if config.verbose {
			log.Printf("%s: completed import of %v addresses from %s\n", s.name, c, aseeder)
		}
	}

	// load ips from the config
	for _, ip := range s.initialIPs {
		if newIP := net.ParseIP(ip); newIP != nil {
			s.addNa(wire.NewNetAddressIPPort(newIP, s.port, 1))
		}
	}

	if len(s.theList) == 0 {
		log.Printf("%s: Error: No ip addresses from seeders so I have nothing to crawl.\n", s.name)
		for _, v := range s.seeders {
			log.Printf("%s: Seeder: %s\n", s.name, v)
		}
	}
}

// runSeeder runs a seeder in an endless goroutine
func (s *dnsseeder) runSeeder(done <-chan struct{}, wg *sync.WaitGroup) {

	defer wg.Done()

	// receive the results from the crawl goroutines
	resultsChan := make(chan *result)

	// serve the good nodes of the cached list right away, instead of none
	// until the first dns update
	s.loadDNS()

	// load data from other seeders so we can start crawling nodes
	s.initSeeder()

	// start initial scan now so we don't have to wait for the timers to fire
	s.startCrawlers(resultsChan)

	// create timing channels for regular tasks
	auditChan := time.NewTicker(time.Minute * auditDelay).C
	crawlChan := time.NewTicker(time.Second * crawlDelay).C
	dnsChan := time.NewTicker(time.Second * dnsDelay).C
	cacheChan := time.NewTicker(time.Minute * cacheDumpDelay).C

	dowhile := true
	for dowhile {
		select {
		case r := <-resultsChan:
			// process a results structure from a crawl
			s.processResult(r)
		case <-dnsChan:
			// update the system with the latest selection of dns records
			s.loadDNS()
		case <-auditChan:
			// keep theList clean and tidy
			s.auditNodes()
		case <-crawlChan:
			// start a scan to crawl nodes
			s.startCrawlers(resultsChan)
		case <-cacheChan:
			// save the list to disk
			s.dumpCache()
		case <-done:
			// done channel closed so exit the select and shutdown the seeder
			dowhile = false
		}
	}
	// save the list, so the next start serves and crawls from it
	s.dumpCache()
	fmt.Printf("shutting down seeder: %s\n", s.name)
	// end the goroutine & defer will call wg.Done()
}

// startCrawlers is called on a time basis to start maxcrawlers new
// goroutines if there are spare goroutine slots available
func (s *dnsseeder) startCrawlers(resultsChan chan *result) {

	s.mtx.RLock()
	defer s.mtx.RUnlock()

	tcount := uint32(len(s.theList))
	if tcount == 0 {
		if config.debug {
			log.Printf("%s - debug - startCrawlers fail: no node available\n", s.name)
		}
		return
	}

	started := make([]uint32, maxStatusTypes)
	totals := make([]uint32, maxStatusTypes)

	// range on a map will not return items in the same order each time
	// so this is a random'ish selection
	for _, nd := range s.theList {

		totals[nd.Status]++

		if nd.CrawlActive {
			continue
		}

		// do we already have enough started at this status
		if started[nd.Status] >= s.maxStart[nd.Status] {
			continue
		}

		// don't crawl a node to quickly
		if (time.Now().Unix() - s.delay[nd.Status]) <= nd.LastTry.Unix() {
			continue
		}

		// all looks good so start a go routine to crawl the remote node
		nd.CrawlActive = true
		nd.CrawlStart = time.Now()

		go crawlNode(resultsChan, s, nd)
		started[nd.Status]++
	}

	// update the global stats in another goroutine to free the main goroutine
	// for other work
	go updateNodeCounts(s, tcount, started, totals)

	// returns and read lock released
}

// processResult will add new nodes to the list and update the status of the crawled node
func (s *dnsseeder) processResult(r *result) {

	var nd *node

	s.mtx.Lock()
	defer s.mtx.Unlock()

	if _, ok := s.theList[r.node]; ok {
		nd = s.theList[r.node]
	} else {
		log.Printf("%s: warning - ignoring results from unknown node: %s\n", s.name, r.node)
		return
	}

	// now nd has been set to a valid pointer we can use it in a defer
	defer crawlEnd(nd)

	// msg is a crawlerror or nil
	if r.msg != nil {
		// update the fact that we have not connected to this node
		nd.LastTry = time.Now()
		nd.ConnectFails++
		nd.StatusStr = r.msg.Error()

		// update the status of this failed node
		switch nd.Status {
		case statusRG:
			// if we are full then any RG failures will skip directly to NG
			if len(s.theList) > s.maxSize {
				nd.Status = statusNG // not able to connect to this node so ignore
			} else {
				if nd.Rating += 25; nd.Rating > 30 {
					nd.Status = statusWG
				}
			}
		case statusCG:
			if nd.Rating += 25; nd.Rating >= 50 {
				nd.Status = statusWG
			}
		case statusWG:
			if nd.Rating += 15; nd.Rating >= 100 {
				nd.Status = statusNG // not able to connect to this node so ignore
			}
		}
		// no more to do so return which will shutdown the goroutine & call
		// the deffered cleanup
		if config.verbose {
			log.Printf("%s: failed crawl node: %s s:r:f: %v:%v:%v %s\n",
				s.name,
				net.JoinHostPort(nd.NA.IP.String(),
					strconv.Itoa(int(nd.NA.Port))),
				nd.Status,
				nd.Rating,
				nd.ConnectFails,
				nd.StatusStr)
		}
		return
	}

	// successful connection and addresses received so mark the node good if
	// it speaks the minimum protocol version. Each dns name filters the good
	// nodes it returns when its records are built. Compare as int64, so a
	// negative version isn't taken for a huge one.
	if int64(r.version) >= int64(s.pver) {
		nd.Status = statusCG
	} else {
		// We can set nodes below the minimum version to was good. This will ensure they stick around for a little
		// while so we can ask them for more addresses. Eventually they will be purged.
		nd.Status = statusWG
	}
	cs := nd.LastConnect
	nd.Rating = 0
	nd.ConnectFails = 0
	nd.LastConnect = time.Now()
	nd.LastTry = nd.LastConnect
	nd.StatusStr = "ok: received remote address list"
	// update the node from the results
	nd.Version = r.version
	nd.Services = r.services
	nd.LastBlock = r.lastBlock
	nd.StrVersion = r.strVersion

	added := 0

	// if we are full then skip adding more possible clients
	if len(s.theList) < s.maxSize {
		// do not accept more than one third of maxSize addresses from one node
		oneThird := int(float64(s.maxSize / 3))

		// loop through all the received network addresses and add to thelist if not present
		for _, na := range r.nas {
			// a new network address so add to the system
			if x := s.addNa(na); x {
				if added++; added > oneThird {
					break
				}
			}
		}
	}

	if config.verbose {
		log.Printf("%s: crawl done: node: %s s:r:f: %v:%v:%v addr: %v:%v CrawlTime: %s Last connect: %v ago\n",
			s.name,
			net.JoinHostPort(nd.NA.IP.String(),
				strconv.Itoa(int(nd.NA.Port))),
			nd.Status,
			nd.Rating,
			nd.ConnectFails,
			len(r.nas),
			added,
			time.Since(nd.CrawlStart).String(),
			time.Since(cs).String())
	}
}

// crawlEnd is run as a defer to make sure node status is correctly updated
func crawlEnd(nd *node) {
	nd.CrawlActive = false
}

// addNa validates and adds a network address to theList
func (s *dnsseeder) addNa(nNa *wire.NetAddress) bool {

	if len(s.theList) > s.maxSize {
		return false
	}

	// generate the key and add to theList
	k := net.JoinHostPort(nNa.IP.String(), strconv.Itoa(int(nNa.Port)))

	if _, dup := s.theList[k]; dup {
		return false
	}
	if nNa.Port <= minPort || nNa.Port >= maxPort {
		return false
	}

	// if the reported timestamp suggests the netaddress has not been seen in the last 24 hours
	// then ignore this netaddress
	if (time.Now().Add(-(time.Hour * 24))).After(nNa.Timestamp) {
		return false
	}

	na := *nNa

	nt := node{
		NA:          &na,
		LastConnect: time.Now(),
		Version:     0,
		Status:      statusRG,
		DNSType:     dnsV4Std,
	}

	// select the dns type based on the remote address type and port
	if x := nt.NA.IP.To4(); x == nil {
		// not ipv4
		if nNa.Port != s.port {
			nt.DNSType = dnsV6Non

			// produce the nonstdIP
			nt.NonstdIP = getNonStdIP(nt.NA.IP, nt.NA.Port)

		} else {
			nt.DNSType = dnsV6Std
		}
	} else {
		// ipv4
		if nNa.Port != s.port {
			nt.DNSType = dnsV4Non

			// force ipv4 address into a 4 byte buffer
			nt.NA.IP = nt.NA.IP.To4()

			// produce the nonstdIP
			nt.NonstdIP = getNonStdIP(nt.NA.IP, nt.NA.Port)
		}
	}

	// add the new node details to theList
	s.theList[k] = &nt

	return true
}

// cachePath returns the path of the cache file of the seeder called name.  The
// .cache.json ending keeps it apart from a network file of the same name,
// also on a filesystem that ignores case.
func cachePath(name string) string {
	return path.Join(config.dataDir, name+".cache.json")
}

// dumpCache saves the list to the seeder's cache file.  It writes a temporary
// file and renames it over the cache, so the cache is never left part written.
func (s *dnsseeder) dumpCache() {
	s.mtx.RLock()
	out, err := json.MarshalIndent(s.theList, "", "    ")
	s.mtx.RUnlock()
	if err != nil {
		log.Printf("error marshalling cache: %s\n", err)
		return
	}

	file := cachePath(s.name)
	f, err := os.CreateTemp(config.dataDir, path.Base(file)+".*")
	if err != nil {
		log.Printf("error writing cache: %s\n", err)
		return
	}
	_, err = f.Write(out)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), file)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		log.Printf("error writing cache: %s\n", err)
	}
}

// getNonStdIP is given an IP address and a port and returns a fake IP address
// that is encoded with the original IP and port number. Remote clients can match
// the two and work out the real IP and port from the two IP addresses.
func getNonStdIP(rip net.IP, port uint16) net.IP {

	b := []byte{0x0, 0x0, 0x0, 0x0}
	crcAddr := crc16(rip.To4())
	b[0] = byte(crcAddr >> 8)
	b[1] = byte((crcAddr & 0xff))
	b[2] = byte(port >> 8)
	b[3] = byte(port & 0xff)

	encip := net.IPv4(b[0], b[1], b[2], b[3])
	if config.debug {
		log.Printf("debug - encode nonstd - realip: %s port: %v encip: %s crc: %x\n", rip.String(), port, encip.String(), crcAddr)
	}

	return encip
}

// crc16 produces a crc16 from a byte slice
func crc16(bs []byte) uint16 {
	var x, crc uint16
	crc = 0xffff

	for _, v := range bs {
		x = crc>>8 ^ uint16(v)
		x ^= x >> 4
		crc = (crc << 8) ^ (x << 12) ^ (x << 5) ^ x
	}
	return crc
}

func (s *dnsseeder) auditNodes() {

	c := 0

	// set this early so for this audit run all NG clients will be purged
	// and space will be made for new, possible CG clients
	iAmFull := len(s.theList) > s.maxSize

	// cgGoal is 75% of the max statusCG clients we can crawl with the current network delay & maxStart settings.
	// This allows us to cycle statusCG users to keep the list fresh
	cgGoal := int(float64(s.delay[statusCG]/crawlDelay) * float64(s.maxStart[statusCG]) * 0.75)
	cgCount := 0

	log.Printf("%s: Audit start. statusCG Goal: %v System Uptime: %s\n", s.name, cgGoal, time.Since(config.uptime).String())

	s.mtx.Lock()
	defer s.mtx.Unlock()

	for k, nd := range s.theList {

		if nd.CrawlActive {
			if time.Now().Unix()-nd.CrawlStart.Unix() >= 300 {
				log.Printf("warning - long running crawl > 5 minutes ====\n- %s status:rating:fails %v:%v:%v crawl start: %s last status: %s\n====\n",
					k,
					nd.Status,
					nd.Rating,
					nd.ConnectFails,
					nd.CrawlStart.String(),
					nd.StatusStr)
			}
		}

		// Audit task is to remove node that we have not been able to connect to
		if nd.Status == statusNG && nd.ConnectFails > maxFails {
			if config.verbose {
				log.Printf("%s: purging node %s after %v failed connections\n", s.name, k, nd.ConnectFails)
			}

			c++
			// remove the map entry and mark the old node as
			// nil so garbage collector will remove it
			s.theList[k] = nil
			delete(s.theList, k)
		}

		// If seeder is full then remove old NG clients and fill up with possible new CG clients
		if nd.Status == statusNG && iAmFull {
			if config.verbose {
				log.Printf("%s: seeder full purging node %s\n", s.name, k)
			}

			c++
			// remove the map entry and mark the old node as
			// nil so garbage collector will remove it
			s.theList[k] = nil
			delete(s.theList, k)
		}

		// check if we need to purge statusCG to freshen the list, but keep
		// the nodes a filtered name returns, which are few among the good
		// nodes, so they don't drop out of its answers
		if nd.Status == statusCG {
			if cgCount++; cgCount > cgGoal && !s.filteredNameReturns(nd) {
				// we have enough statusCG clients so purge remaining to cycle through the list
				if config.verbose {
					log.Printf("%s: seeder cycle statusCG - purging node %s\n", s.name, k)
				}

				c++
				// remove the map entry and mark the old node as
				// nil so garbage collector will remove it
				s.theList[k] = nil
				delete(s.theList, k)
			}
		}
	}

	log.Printf("%s: Audit complete. %v nodes purged. %v total nodes\n", s.name, c, len(s.theList))
}

// loadDNS loads the dns records with time based test data
func (s *dnsseeder) loadDNS() {
	updateDNS(s)
}

// getSeederByName returns a pointer to the seeder based on its name or nil if not found
func getSeederByName(name string) *dnsseeder {
	for _, s := range config.seeders {
		if s.name == name {
			return s
		}
	}
	return nil
}

// filteredNameReturns returns true if one of the seeder's names that returns
// only some of the full nodes returns nd.
func (s *dnsseeder) filteredNameReturns(nd *node) bool {
	for i := range s.names {
		if s.names[i].filtersFullNodes() && s.names[i].returns(nd) {
			return true
		}
	}
	return false
}

// isDuplicateSeeder returns true if a loaded seeder has the seeder's name, or
// the seeder serves a dns name twice, or one that a loaded seeder serves
func isDuplicateSeeder(s *dnsseeder) (bool, error) {

	// The seeders are kept by name, and their cache files are named after
	// them, so a seeder with a loaded one's name, in any case, would replace
	// it and share its cache.
	for _, v := range config.seeders {
		if strings.EqualFold(v.name, s.name) {
			return true, fmt.Errorf("duplicate seeder name %s, already loaded as %s", s.name, v.name)
		}
	}

	// check for duplicate seeders with the same details
	for i, n := range s.names {
		for _, m := range s.names[:i] {
			if m.host == n.host {
				return true, fmt.Errorf("duplicate DNS name %s in %s", n.host, s.name)
			}
		}
		for _, v := range config.seeders {
			for _, m := range v.names {
				if m.host == n.host {
					return true, fmt.Errorf("duplicate DNS names. Already loaded %s for %s so can not be used for %s", n.host, v.name, s.name)
				}
			}
		}
	}
	return false, nil
}
