# dnsseeder Kubernetes Configs

The following guide will walk you through creating a dnsseeder instance within GKE (Google Kubernetes Engine).

Steps:

1. Create the persistent disk the seeders keep their caches on, `dnsseeder-data`, in your cluster's zone: `gcloud compute disks create dnsseeder-data --size 10GB --zone <zone>`. Kubernetes formats it when it first mounts it.
2. Run `kubectl create -f /path/to/kube/`.
3. Look up the `dnsseeder-udp` service in the web UI to get your public IP, and reserve it as a static address, so it outlives the service.
4. Edit `dnsseeder-udp-srv.yml` and set `loadBalancerIP` to your public IP.
5. Point the `NS` records of your seed names to a host with that IP, as the main README's Configuring DNS section describes.
6. Profit!

The deployment names the network configs to load in its `-netfile` argument. When a new image renames or merges configs, apply the deployment with it, `kubectl apply -f /path/to/kube/dnsseeder-deployment.yml`, as the old list would name files the image no longer has.
