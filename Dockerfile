# Dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app

# Install git untuk go mod (jika diperlukan)
RUN apk add --no-cache git

# Copy go mod files first untuk caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build dengan flags untuk mengurangi penggunaan memory
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o main .

FROM alpine:latest
WORKDIR /app

# Install ca-certificates untuk HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/main .
COPY --from=builder /app/.env .

EXPOSE 4000
CMD ["./main"]