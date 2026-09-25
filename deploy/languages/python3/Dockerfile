FROM python:3.12-alpine

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 1000 runner \
    && adduser -D -H -u 1000 -G runner runner

WORKDIR /workspace
USER runner:runner
