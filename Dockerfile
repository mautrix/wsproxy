FROM golang:1-alpine AS builder

COPY . /build
WORKDIR /build
RUN CGO_ENABLED=0 go build -o /usr/bin/mautrix-wsproxy

FROM scratch

COPY --from=builder /usr/bin/mautrix-wsproxy /usr/bin/mautrix-wsproxy

ENV LISTEN_ADDRESS=:29331

CMD ["/usr/bin/mautrix-wsproxy", "-config", "env"]
