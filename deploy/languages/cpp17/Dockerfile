FROM alpine:3.22

RUN apk add --no-cache ca-certificates g++ musl-dev tzdata \
    && addgroup -g 1000 runner \
    && adduser -D -H -u 1000 -G runner runner

WORKDIR /workspace
USER runner:runner
