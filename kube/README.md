# dnsseeder Kubernetes Configs

The following guide will walk you through creating a dnsseeder instance within GKE (Google Container Engine).

Steps:
1. Run `kubectl create -f /path/to/kube/`.
2. Lookup the `dnsseeder-udp` service in the web-ui to get your public ip.
3. Edit `dnsseeder-udp-srv.yml` and set `loadBalancerIP` to your public IP.
4. Profit!
