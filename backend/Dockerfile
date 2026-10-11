# syntax=docker/dockerfile:1
#
# tf-automation backend: the API server, plus seed-admin (create the first
# platform admin) and golang-migrate (apply db/migrations) in the same image.
#
#   docker build -t tf-automation-backend ./backend
#   docker run --env-file .env -p 8080:8080 tf-automation-backend
#   docker run --rm --entrypoint migrate tf-automation-backend -path /app/migrations -database "$DATABASE_URL" up
#   docker run --rm --entrypoint seed-admin -e DATABASE_URL tf-automation-backend -username admin -password '...'

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    go build -trimpath -ldflags="-s -w" -o /out/seed-admin ./cmd/seed-admin && \
    GOBIN=/out go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget && adduser -D -H -u 10001 app
COPY --from=build /out/ /usr/local/bin/
COPY db/migrations /app/migrations

USER app
ENV HTTP_ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/server"]
