# dnsseeder Kubernetes Configs

The following guide will walk you through creating a dnsseeder instance within GKE (Google Container Engine).

Steps:
1. Run `kubectl create -f /path/to/kube/dnsseeder-tcp-srv.yml`.
2. Lookup the `dnsseeder-tcp` service in the web-ui to get your public ip.
3. Edit `dnsseeder-tcp-srv.yml` and `dnsseeder-udp-srv.yml` and set `loadBalancerIP` to your public IP.
4. Run `kubectl apply -f /path/to/kube/`.
3. Profit!
