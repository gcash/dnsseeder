dnsseeder
=========

[![Build Status](https://github.com/gcash/dnsseeder/actions/workflows/main.yml/badge.svg?branch=master)](https://github.com/gcash/dnsseeder/actions/workflows/main.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue.svg)](http://godoc.org/github.com/gcash/dnsseeder)

A Bitcoin Cash network crawler and DNS seeder written in Go.

Originally forked from https://github.com/gombadi/dnsseederd

## Features

* Supports multiple networks. You can run multiple seeders off one ip address.
* Uses Go Language so it can easily be compiled and run on multiple platforms.
* Minimal resource requirements. Will easily seed multiple networks on a Raspberry Pi 1 Mobel B+
* Restricts the number of addresses accepted from any one node.
* Cycle through working nodes to keep the active list fresh
* Reduces bandwidth usage on nodes if it has many working nodes already in the system.
* Ability to generate and edit your own seeder config file to support new networks.

## Installing

Simply use go get to download the code:

    $ go get github.com/gcash/dnsseeder

## Usage

    $ dnsseeder -v -netfile <filename1,filename2>

If you want to be able to view the web interface then add -w port for the web server to listen on. If this is not provided then no web interface will be available. With the web site running you can then access the site by http://localhost:port/summary

**NOTE -** For security reasons the web server will only listen on localhost so you will need to either use an ssh tunnel or proxy requests via a web server like Caddy.

```
Command line Options:
-netfile comma seperated list of json network config files to load
-j write a sample network config file in json format and exit.
-p port to listen on for DNS requests
-d Produce debug output
-v Produce verbose output
-w Port to listen on for Web Interface
```

## Docker

Building and running `dnsseeder` in docker is quite painless. To build the image:

```
docker build . -t dnsseeder
```

To run the image with both TCP and UDP support:

```
docker run -p 8053:8053/udp dnsseeder
```

This starts the dnsseeder on port 8053. You will need root to bind to
port 53 directly.

## Configuring DNS

If you want to seed peers on `seed.example.com`, say, and this software is running on `vps.example.com` then you need to put a `NS` record into the `seed.example.com` DNS record pointing to `vps.example.com`.

Set `NameServer` in the network config file to the host the `NS` record points to, `vps.example.com` here. The seeder answers queries for the `NS` and `SOA` records of its DNS name with it, and with `.` when it is not set. `Mbox`, optional, is the zone administrator's email address for the `SOA` record, with the `@` replaced by a period, such as `admin.example.com`; without it the record holds `.`.

`Pver` is the lowest protocol version a node must report to be returned; the bchd.cash configs require 70016. `ServiceFilter` and `UserAgentFilter` limit the nodes a seeder returns to those that advertise every listed service and whose user agent contains one of the listed strings, ignoring case. The `x101` configs return bchd nodes that serve compact filters, with `"UserAgentFilter": ["bchd:0.22."]`, which matches every 0.22 release.

A seeder also leaves out nodes whose reported height is more than 144 blocks, a day, from the median height of its good nodes. That drops nodes of other chains that kept the network's magic, such as BSV and eCash nodes, which were over 1,800 blocks behind BCH nodes in October 2026, and nodes that stopped syncing.

Clients ask a seed's name for full nodes, and `x<flags>.<name>` for nodes with the services in the hexadecimal `<flags>`; bchd, for one, takes every address it gets from the name for a full node. So a seeder on a plain name needs at least `"ServiceFilter": ["NodeNetwork"]`, and one on an `x<flags>` name every service in its flags, as the `x101` configs have `NodeNetwork` (`1`) and `NodeCF` (`100`).

## RUNNING AS NON-ROOT

Typically, you'll need root privileges to listen to port 53 (name service).

One solution is using an iptables rule (Linux only) to redirect it to
a non-privileged port:

$ iptables -t nat -A PREROUTING -p udp --dport 53 -j REDIRECT --to-port 8053

If properly configured, this will allow you to run dnsseeder in userspace, using the -p 8053 option.

## License
Apache 2.0

For the DNS library license see https://github.com/miekg/dns
