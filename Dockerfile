# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
FROM golang

LABEL maintainer="Josh Ellithorpe <quest@mac.com>"

WORKDIR /go/src/github.com/gcash/dnsseeder

# Copy the local package files to the container's workspace.
COPY . .

# Build static binary
RUN CGO_ENABLED=0 go build --ldflags '-extldflags "-static"' -o /bin/dnsseeder .

# Create final image
FROM scratch
WORKDIR /var/lib/dnsseeder

# Document that the service listens on port 8053.
EXPOSE 8053/udp

# Copy SSL CA certa, configs, and the binary from build image to final image
COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=0 /go/src/github.com/gcash/dnsseeder/configs/ ./
COPY --from=0 /bin/dnsseeder /bin/dnsseeder

# Start dnsseeder
ENTRYPOINT ["/bin/dnsseeder", "-s", "-d", "-netfile=mainnet-all.json,mainnet-filtered.json,mainnet-node-cf.json"]
