# Build the server binary, then ship it on a small base.
#
# CGO stays off: the SQLite driver is pure Go, so the binary is static and the
# runtime image needs no toolchain. Alpine rather than distroless because
# `kamal app exec` wants a shell for the admin commands.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Dependencies first, so a source-only change reuses this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/habitat ./cmd/habitat

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 habitat \
    && mkdir -p /var/lib/habitat \
    && chown habitat:habitat /var/lib/habitat

COPY --from=build /out/habitat /usr/local/bin/habitat

USER habitat
WORKDIR /var/lib/habitat

# The run history lives here, on a volume — a deploy must not discard it.
VOLUME ["/var/lib/habitat"]
ENV HABITAT_DB=/var/lib/habitat/habitat.db
EXPOSE 7878

# Binds beyond loopback, so the server requires an account to read anything.
CMD ["sh", "-c", "habitat serve --db \"$HABITAT_DB\" --addr 0.0.0.0:${PORT:-7878}"]
