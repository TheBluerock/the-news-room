FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /analytics ./cmd/main.go

FROM alpine:3.20
COPY --from=builder /analytics /analytics
EXPOSE 8080 8090
ENTRYPOINT ["/analytics"]
