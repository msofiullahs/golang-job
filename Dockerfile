FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo dev)" -o /goqueue ./cmd/goqueue

FROM gcr.io/distroless/static-debian12
COPY --from=builder /goqueue /goqueue
EXPOSE 8080
ENTRYPOINT ["/goqueue"]
CMD ["server"]
