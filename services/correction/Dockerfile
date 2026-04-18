FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /correction ./cmd/main.go

FROM gcr.io/distroless/static-debian12
COPY --from=builder /correction /correction
EXPOSE 8090
ENTRYPOINT ["/correction"]
