#!/bin/sh
# Generates gzipped man pages for packaging (used by GoReleaser).
set -e
rm -rf manpages
go run ./cmd/envhaul gen man manpages
gzip -9n manpages/*.1
