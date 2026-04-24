# Stage 1: Build frontend
FROM oven/bun:1-alpine AS frontend-builder
WORKDIR /app/web-app
COPY web-app/package.json web-app/bun.lock ./
RUN bun install --frozen-lockfile
COPY web-app/ ./
RUN bun run build

# Stage 2: Build Go binary (no CGO needed)
FROM golang:1.26-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY --from=frontend-builder /app/web-app/dist ./web-app/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o paperless-tagger .

# Stage 3: Runtime image
FROM alpine:3.20
RUN apk add --no-cache poppler-utils ca-certificates tzdata
WORKDIR /data
COPY --from=go-builder /app/paperless-tagger /usr/local/bin/paperless-tagger

EXPOSE 8080
ENV DATA_DIR=/data
ENV GIN_MODE=release

VOLUME ["/data"]

ENTRYPOINT ["paperless-tagger"]
