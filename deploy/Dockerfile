# One image for every role and every client: a client is configuration and
# assets, not a build. `--role web|worker|all` selects behaviour at boot.
#
# Both base images are pinned by digest and named by tag beside it. A tag is a
# pointer somebody can move — golang:1.26 is a different set of bytes every
# patch release — and an image built from a moving base is an image nobody can
# rebuild. The tag stays in the line because the digest alone says nothing about
# what it is; the digest is what the build resolves.
#
# Bumping either is one commit: docker buildx imagetools inspect <tag> prints
# the digest to paste.
FROM golang:1.26@sha256:9d2f36f06329b2a141b9db99ffa32765cf695ee57b813ca29e245e8670bcbfff AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/platformkit ./apps/platformkit

FROM gcr.io/distroless/static-debian12@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2
COPY --from=build /out/platformkit /platformkit
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/platformkit"]
