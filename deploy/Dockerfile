# One image for every role and every client: a client is configuration and
# assets, not a build. `--role web|worker|all` selects behaviour at boot.
#
# This does not build yet: ./apps/platformkit arrives in stage E2.

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/platformkit ./apps/platformkit

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/platformkit /platformkit
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/platformkit"]
