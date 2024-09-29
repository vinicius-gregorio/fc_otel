FROM golang:1.23.1 as builder
WORKDIR /app
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build --ldflags="-w -s" -o ms cmd/main.go

FROM alpine:latest
COPY --from=builder /app/ms /app/ms
CMD ["/app/ms"]