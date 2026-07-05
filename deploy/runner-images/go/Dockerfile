FROM golang:1.24-alpine

RUN apk add --no-cache ca-certificates git tzdata \
    && addgroup -g 1000 runner \
    && adduser -D -H -u 1000 -G runner runner

WORKDIR /workspace
USER runner:runner
