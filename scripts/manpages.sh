#!/bin/sh
# Generates gzipped man pages for packaging (used by GoReleaser).
set -e
# Date the pages by the commit rather than today, so rebuilding the same
# commit gives identical pages (cobra honors SOURCE_DATE_EPOCH).
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct)}
export SOURCE_DATE_EPOCH
rm -rf manpages
go run ./cmd/envhaul gen man manpages
gzip -9n manpages/*.1
