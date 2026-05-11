FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOFLAGS=-mod=mod go build -o /sanity ./cmd/main.go

FROM alpine:3.20
COPY --from=builder /sanity /sanity
EXPOSE 8088 8090
ENTRYPOINT ["/sanity"]
