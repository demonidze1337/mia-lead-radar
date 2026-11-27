# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/internal/server/static/dist ./internal/server/static/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mia ./cmd/mia

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S mia \
    && adduser -u 10001 -S -G mia -h /app mia \
    && mkdir -p /app/data \
    && chown -R mia:mia /app
WORKDIR /app
COPY --from=backend /out/mia /app/mia
ENV HTTP_ADDR=:8080 \
    DATA_DIR=/app/data \
    WEBAPP_BASE_URL=http://localhost:8080
EXPOSE 8080
VOLUME ["/app/data"]
USER mia:mia
ENTRYPOINT ["/app/mia"]
