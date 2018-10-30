# Start from a Debian image with the latest version of Go installed
# and a workspace (GOPATH) configured at /go.
FROM golang

LABEL maintainer="Josh Ellithorpe <quest@mac.com>"

# Copy the local package files to the container's workspace.
ADD . /go/src/github.com/gcash/dnsseeder

# Switch to the correct working directory.
WORKDIR /go/src/github.com/gcash/dnsseeder

# Restore vendored packages.
RUN go get -u github.com/golang/dep/cmd/dep
RUN dep ensure

# Build the code and the cli client.
RUN go install .

# Set the start command.
# -s -d -netfile=mainnet-all.json,mainnet-filtered.json
ENTRYPOINT ["dnsseeder", "-s", "-d", "-netfile=configs/mainnet-all.json,configs/mainnet-filtered.json"]

# Document that the service listens on port 8053.
EXPOSE 8053
EXPOSE 8053/udp
