FROM golang:1.26-alpine AS build
ENV RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go vet ./... && CGO_ENABLED=0 go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gw ./cmd/gw
FROM scratch
# XBL auth does HTTPS OIDC discovery at listen time — scratch needs root CAs.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/gw /gw
USER 1000:1000
EXPOSE 19132/udp
ENTRYPOINT ["/gw"]
