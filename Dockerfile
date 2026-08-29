# Built by GoReleaser (see .goreleaser.yaml's `dockers:` section), which
# cross-compiles the linux/amd64 binary and stages it into this Dockerfile's
# build context before running `docker build` -- there's no `go build` here,
# so `docker build .` on its own won't work outside a GoReleaser run
# (`goreleaser release --snapshot --clean` builds and stages it locally).
#
# modernc.org/sqlite is pure Go (ADR-0002: chosen specifically so the build
# needs no C toolchain and the binary needs no C runtime at all), so a
# static, distroless base is enough -- no libc, no shared libraries.
FROM gcr.io/distroless/static-debian12:nonroot

# dockers_v2 stages each platform's binary under a $TARGETPLATFORM-named
# directory in the build context (e.g. linux/amd64/eve-trader), not at the
# context root, so the copy source must use that buildx-provided arg.
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/eve-trader /eve-trader

EXPOSE 8080

ENTRYPOINT ["/eve-trader"]
