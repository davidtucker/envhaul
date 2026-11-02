# Release image, built by GoReleaser (dockers_v2) from prebuilt binaries.
# Usage: docker run --rm -v "$PWD:/work" -w /work -v "$HOME/.aws:/home/nonroot/.aws:ro" ghcr.io/davidtucker/envhaul sync
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/envhaul /usr/bin/envhaul
ENTRYPOINT ["/usr/bin/envhaul"]
