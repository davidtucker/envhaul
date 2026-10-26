# Release image, built by GoReleaser (dockers_v2) from prebuilt binaries.
# Usage: docker run --rm -v "$PWD:/work" -w /work -v "$HOME/.aws:/home/nonroot/.aws:ro" ghcr.io/davidtucker/envhaul sync
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/envhaul /usr/bin/envhaul
ENTRYPOINT ["/usr/bin/envhaul"]
