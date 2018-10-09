/*
This application provides a DNS seeder service to the Bitcoin Cash network.

This application crawls the Network for active clients and records their ip address and port. It then replies to DNS queries with this information.

Features:
- Preconfigured support for Bitcoin Cash mainnet and testnet. Use -net <network> to load config data.
- Supports ipv4 & ipv6 addresses
- Revisits clients on a configurable time basis to make sure they are still available
- Low memory & cpu requirements
*/
package main
