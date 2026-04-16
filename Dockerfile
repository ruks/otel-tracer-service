# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
RUN CGO_ENABLED=0 go build -o trace-sender .

# Run stage
FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/trace-sender .
ENV OTEL_ENDPOINT=localhost:4317
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["./trace-sender"]

