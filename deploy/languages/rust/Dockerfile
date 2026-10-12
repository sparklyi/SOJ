FROM rust:1.99-alpine

# rustc links through the system toolchain; musl-dev is the linker's headers.
RUN apk add --no-cache gcc musl-dev

WORKDIR /workspace
