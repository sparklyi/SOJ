FROM golang:1.22.5 AS builder
LABEL authors="sparkyi@foxmail.com"

WORKDIR /app
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o main

FROM alpine:latest
COPY --from=builder /app/main .
COPY config ./config
EXPOSE 8888
CMD ["./main"]