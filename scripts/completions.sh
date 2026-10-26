#!/bin/sh
# Generates shell completion scripts for packaging (used by GoReleaser).
set -e
rm -rf completions
mkdir completions
for sh in bash zsh fish powershell; do
  go run ./cmd/envhaul completion "$sh" > "completions/envhaul.$sh"
done
