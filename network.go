package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/gcash/bchd/wire"
)

// JNetwork is the exported struct that is read from the network file
type JNetwork struct {
	Name            string
	Desc            string
	ID              string
	Port            uint16
	Pver            uint32
	DNSName         string
	NameServer      string
	Mbox            string
	TTL             uint32
	InitialIPs      []string
	Seeder1         string
	Seeder2         string
	Seeder3         string
	ServiceFilter   []string
	UserAgentFilter []string
	DNSNames        []JDNSName
}

// JDNSName is one of several dns names a network file serves, with the filters
// of the nodes it returns.  A network file sets either DNSNames, or DNSName
// and, for it, ServiceFilter and UserAgentFilter.
type JDNSName struct {
	Name            string
	ServiceFilter   []string
	UserAgentFilter []string
}

func createNetFile() {
	// create a standard json template file that can be loaded into the app

	// create a struct to encode with json
	jnw := &JNetwork{
		ID:         "0xabcdef01",
		Port:       1234,
		Pver:       70001,
		TTL:        600,
		DNSName:    "seeder.example.com",
		NameServer: "nameserver.example.com",
		Name:       "SeederNet",
		Mbox:       "admin.example.com", // @ symbol replaced with period
		Desc:       "Description of SeederNet",
		Seeder1:    "seeder1.example.com",
		Seeder2:    "seed1.bob.com",
		Seeder3:    "seed2.example.com",
	}

	f, err := os.Create("dnsseeder.json")
	if err != nil {
		log.Printf("error creating template file: %v\n", err)
	}
	defer func() { _ = f.Close() }()

	j, jerr := json.MarshalIndent(jnw, "", " ")
	if jerr != nil {
		log.Printf("error parsing json: %v\n", err)
	}
	_, ferr := f.Write(j)
	if ferr != nil {
		log.Printf("error writing to template file: %v\n", err)
	}
}

func loadNetwork(fName string) (*dnsseeder, error) {
	nwFile, err := os.Open(fName)
	if err != nil {
		return nil, fmt.Errorf("error reading network file: %v", err)
	}

	defer func() { _ = nwFile.Close() }()

	var jnw JNetwork

	jsonParser := json.NewDecoder(nwFile)
	if err = jsonParser.Decode(&jnw); err != nil {
		return nil, fmt.Errorf("error decoding network file: %v", err)
	}

	return initNetwork(jnw)
}

func initNetwork(jnw JNetwork) (*dnsseeder, error) {
	if jnw.Port == 0 {
		return nil, fmt.Errorf("invalid port supplied: %v", jnw.Port)

	}

	// the names to serve: DNSNames, or DNSName with the filters
	jnames := jnw.DNSNames
	if len(jnames) == 0 {
		jnames = []JDNSName{{
			Name:            jnw.DNSName,
			ServiceFilter:   jnw.ServiceFilter,
			UserAgentFilter: jnw.UserAgentFilter,
		}}
	} else if jnw.DNSName != "" || len(jnw.ServiceFilter) > 0 || len(jnw.UserAgentFilter) > 0 {
		return nil, fmt.Errorf("DNSNames is set with DNSName or its filters")
	}

	// init the seeder
	seeder := &dnsseeder{}
	seeder.theList = make(map[string]*node)
	seeder.port = jnw.Port
	seeder.pver = jnw.Pver
	seeder.ttl = jnw.TTL
	seeder.name = jnw.Name
	seeder.desc = jnw.Desc
	for _, jn := range jnames {
		if jn.Name == "" {
			return nil, fmt.Errorf("no DNS hostname supplied")
		}
		seeder.names = append(seeder.names, dnsName{
			host:            strings.ToLower(jn.Name),
			serviceFilter:   parseServices(jn.ServiceFilter),
			userAgentFilter: jn.UserAgentFilter,
		})
	}
	seeder.nameServer = jnw.NameServer
	seeder.mbox = jnw.Mbox

	if jnw.NameServer == "" {
		var hosts []string
		for _, n := range seeder.names {
			hosts = append(hosts, n.host)
		}
		log.Printf("%s: warning: NameServer is not set, so the NS and SOA records for %s name \".\" instead of this server\n",
			jnw.Name, strings.Join(hosts, ", "))
	}

	// conver the network magic number to a Uint32
	t1, err := strconv.ParseUint(jnw.ID, 0, 32)
	if err != nil {
		return nil, fmt.Errorf("error converting network magic number: %v", err)
	}
	seeder.id = wire.BitcoinNet(t1)

	seeder.initialIPs = jnw.InitialIPs

	// Load up the seeders. Due to an odd config format
	// that accepts a different key per seeder, we need to
	// do some empty value checks...
	seeder.seeders = []string{}
	if jnw.Seeder1 != "" {
		seeder.seeders = append(seeder.seeders, jnw.Seeder1)
	}
	if jnw.Seeder2 != "" {
		seeder.seeders = append(seeder.seeders, jnw.Seeder2)
	}
	if jnw.Seeder3 != "" {
		seeder.seeders = append(seeder.seeders, jnw.Seeder3)
	}

	// add some checks to the start & delay values to keep them sane
	seeder.maxStart = []uint32{20, 20, 20, 30}
	seeder.delay = []int64{210, 789, 234, 1876}
	seeder.maxSize = 100000

	// initialize the stats counters
	seeder.counts.NdStatus = make([]uint32, maxStatusTypes)
	seeder.counts.NdStarts = make([]uint32, maxStatusTypes)
	seeder.counts.DNSCounts = make([]uint32, maxDNSTypes)

	// some sanity checks on the loaded config options
	if seeder.ttl < 60 {
		seeder.ttl = 60
	}

	if dup, err := isDuplicateSeeder(seeder); dup {
		return nil, err
	}

	cacheFile, err := os.Open(cachePath(seeder.name))
	if err == nil {
		defer func() { _ = cacheFile.Close() }()
		jsonParser := json.NewDecoder(cacheFile)
		if err = jsonParser.Decode(&seeder.theList); err != nil {
			log.Printf("Error decoding cache file: %v", err)
		}
		// no crawl survives a restart, and a node the cache caught
		// mid-crawl would otherwise never be crawled again
		for _, nd := range seeder.theList {
			nd.CrawlActive = false
		}
		log.Printf("Loaded %d nodes from %s cache\n", len(seeder.theList), seeder.name)
	}

	return seeder, nil
}

// parseServices returns the service flags of a service filter's names, and
// ignores names it doesn't know.
func parseServices(names []string) []wire.ServiceFlag {
	var services []wire.ServiceFlag
	for _, service := range names {
		switch strings.ToLower(service) {
		case "nodenetwork":
			services = append(services, wire.SFNodeNetwork)
		case "nodegetutxo":
			services = append(services, wire.SFNodeGetUTXO)
		case "nodebloom":
			services = append(services, wire.SFNodeBloom)
		case "nodexthin":
			services = append(services, wire.SFNodeXthin)
		case "nodecf":
			services = append(services, wire.SFNodeCF)
		case "nodebitcoincash":
			services = append(services, wire.SFNodeBitcoinCash)
		}
	}
	return services
}
