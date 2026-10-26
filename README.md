dnsseeder
=========

[![Build Status](https://github.com/gcash/dnsseeder/actions/workflows/main.yml/badge.svg?branch=master)](https://github.com/gcash/dnsseeder/actions/workflows/main.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue.svg)](http://godoc.org/github.com/gcash/dnsseeder)

A Bitcoin Cash network crawler and DNS seeder written in Go.

Originally forked from https://github.com/gombadi/dnsseeder

## Features

- Supports multiple networks. You can run multiple seeders off one ip address.
- Serves several DNS names per network from one crawl, each returning the nodes with the services, user agent and protocol version it asks for.
- Leaves out nodes of other chains that share the network's magic, such as BSV and eCash nodes, and nodes that stopped syncing.
- Answers whatever the case of the name asked for, as resolvers such as Google Public DNS randomize it.
- Uses Go Language so it can easily be compiled and run on multiple platforms.
- Minimal resource requirements. Will easily seed multiple networks on a Raspberry Pi 1 Model B+
- Restricts the number of addresses accepted from any one node.
- Cycle through working nodes to keep the active list fresh
- Reduces bandwidth usage on nodes if it has many working nodes already in the system.
- Ability to generate and edit your own seeder config file to support new networks.

## Installing

Install it with Go:

    $ go install github.com/gcash/dnsseeder@latest

## Usage

    $ dnsseeder -v -netfile <filename1,filename2>

If you want to be able to view the web interface then add -w port for the web server to listen on. If this is not provided then no web interface will be available. With the web site running you can then access the site by http://localhost:port/summary

**NOTE -** For security reasons the web server will only listen on localhost so you will need to either use an ssh tunnel or proxy requests via a web server like Caddy.

```
Command line Options:
-netfile comma separated list of json network config files to load
-datadir directory to keep the seeders' caches in, ~/.dnsseeder on Linux by default
-j write a sample network config file, dnsseeder.json, and exit
-p port to listen on for DNS requests, 8053 by default
-s Produce stats output
-v Produce verbose output, and stats output
-d Produce debug output, and verbose and stats output
-w Port to listen on for Web Interface
```

## Docker

Building and running `dnsseeder` in docker is quite painless. To build the image:

```
docker build . -t dnsseeder
```

To run the image:

```
docker run -p 8053:8053/udp dnsseeder
```

This starts the dnsseeder on port 8053 with `mainnet.json`, the bchd.cash mainnet seeder. Arguments after the image name are added to its command, so `-netfile` selects other network configs, and `-datadir` on a volume keeps the seeders' caches across restarts:

```
docker run -p 8053:8053/udp -v /srv/dnsseeder:/data dnsseeder -datadir /data -netfile /data/seed.json
```

You will need root to bind to port 53 directly.

## Configuring DNS

If you want to seed peers on `seed.example.com`, say, and this software is running on `vps.example.com` then you need to put a `NS` record into the `seed.example.com` DNS record pointing to `vps.example.com`.

Set `NameServer` in the network config file to the host the `NS` record points to, `vps.example.com` here. The seeder answers queries for the `NS` and `SOA` records of its DNS names with it, and with `.` when it is not set. `Mbox`, optional, is the zone administrator's email address for the `SOA` record, with the `@` replaced by a period, such as `admin.example.com`; without it the record holds `.`.

## Network configs

A network config is a JSON file that describes a network to crawl and the DNS names to serve its nodes on. `-j` writes an example, and the `configs` directory holds the configs of the bchd.cash seeders, one per network with its plain, `x105` and `x101` names, and templates, `*.json.template`, to start a config for your own seeder from.

| Field                           | Meaning                                                                                                                                                                |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Name`                          | The seeder's name, in logs and on the web interface. Its cache is `<Name>.cache.json` in the data directory, so no two loaded configs may share a name, ignoring case. |
| `Desc`                          | A description of the network.                                                                                                                                          |
| `Id`                            | The network's magic number, such as `0xe8f3e1e3` for mainnet.                                                                                                          |
| `Port`                          | The network's default port. Nodes on other ports are only returned, for IPv4, under `nonstd.` names, with the port encoded in a second address.                        |
| `Pver`                          | The lowest protocol version a node must report to be returned.                                                                                                         |
| `DNSName`                       | The DNS name to serve, for a config with one name.                                                                                                                     |
| `ServiceFilter`                 | The services a node must advertise to be returned under `DNSName`, out of `NodeNetwork`, `NodeGetUTXO`, `NodeBloom`, `NodeXthin`, `NodeCF` and `NodeBitcoinCash`.      |
| `UserAgentFilter`               | Strings one of which a node's user agent must contain, ignoring case, to be returned under `DNSName`.                                                                  |
| `DNSNames`                      | Instead of `DNSName` and its filters, several names to serve from one crawl, each with its own `Name`, `ServiceFilter` and `UserAgentFilter`.                          |
| `NameServer`                    | The host the names' `NS` records point to, as above.                                                                                                                   |
| `Mbox`                          | Optional: the zone administrator's email address, as above.                                                                                                            |
| `TTL`                           | The TTL of the address records, in seconds, at least 60.                                                                                                               |
| `Seeder1`, `Seeder2`, `Seeder3` | DNS seeds to take the first nodes to crawl from.                                                                                                                       |
| `InitialIPs`                    | Addresses of nodes to start crawling from as well.                                                                                                                     |

A node is good once it answers a crawl and reports at least `Pver`, and each name returns the good nodes that pass its filters:

```json
"DNSNames": [
  {"Name": "seed.example.com", "ServiceFilter": ["NodeNetwork"]},
  {"Name": "x101.seed.example.com", "ServiceFilter": ["NodeNetwork", "NodeCF"]}
]
```

The bchd.cash configs require protocol 70016, and their `x101` names return bchd nodes that serve compact filters, with `"UserAgentFilter": ["bchd:0.22."]`, which matches every 0.22 release.

Clients ask a seed's name for full nodes, and `x<flags>.<name>` for nodes with the services in the hexadecimal `<flags>`; bchd, for one, takes every address it gets from the name for a full node. So a plain name needs at least `"ServiceFilter": ["NodeNetwork"]`, and an `x<flags>` name every service in its flags, as the `x101` names have `NodeNetwork` (`1`) and `NodeCF` (`100`).

A seeder also leaves out nodes whose reported height is more than 144 blocks, a day, from the median height of its good nodes. That drops nodes of other chains that kept the network's magic, such as BSV and eCash nodes, which were over 1,800 blocks behind BCH nodes in October 2026, and nodes that stopped syncing.

## Running as non-root

Typically, you'll need root privileges to listen to port 53 (name service).

One solution is using an iptables rule (Linux only) to redirect it to a non-privileged port:

```
iptables -t nat -A PREROUTING -p udp --dport 53 -j REDIRECT --to-port 8053
```

If properly configured, this will allow you to run dnsseeder in userspace, using the -p 8053 option.

## License

Apache 2.0

For the DNS library license see https://github.com/miekg/dns
