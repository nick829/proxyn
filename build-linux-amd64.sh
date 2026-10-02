#!/bin/sh
set -eu
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0
go mod tidy
go build -trimpath -ldflags='-s -w' -o bedrock-proxy .
echo "Built ./bedrock-proxy for Linux amd64"
