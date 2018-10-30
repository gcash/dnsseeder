# dnsseeder
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
    $ dep ensure

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
docker run -p 5353:53 -p 5353:53/udp dnsseeder
```

This starts the dnsseeder on port 5353. You will need root to bind to
port 53 directly.

## Configuring DNS

If you want to seed peers on `seed.example.com`, say, and this software is running on `vps.example.com` then you need to put a `NS` record into the `seed.example.com` DNS record pointing to `vps.example.com`.

## RUNNING AS NON-ROOT

Typically, you'll need root privileges to listen to port 53 (name service).

One solution is using an iptables rule (Linux only) to redirect it to
a non-privileged port:

$ iptables -t nat -A PREROUTING -p udp --dport 53 -j REDIRECT --to-port 5353

If properly configured, this will allow you to run dnsseeder in userspace, using
the -p 5353 option.

## License
Apache 2.0

For the DNS library license see https://github.com/miekg/dns
