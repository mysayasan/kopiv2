# syntax=docker/dockerfile:1

FROM golang:1.26 AS builder
WORKDIR /src
ARG APP=mymatasan

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/kopiv2-server ./cmd/${APP}

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ARG APP=mymatasan

WORKDIR /app/apps/${APP}

COPY --from=builder /out/kopiv2-server /app/kopiv2-server
COPY apps ./apps

ENV ENVIRONMENT=dev
ENV SERVER_ADDR=:3000
ENV SERVER_USE_TLS=false

EXPOSE 3000

ENTRYPOINT ["/app/kopiv2-server"]
