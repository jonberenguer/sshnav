# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Cache dependency downloads separately from source
COPY go.mod go.sum* ./

# Copy source and build a static binary
COPY . .
RUN go mod tidy && go mod download
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o sshnav \
      .

# ---- export stage ----
# Scratch image — just extracts the binary via `docker cp` or `--output`
FROM scratch
COPY --from=builder /build/sshnav /sshnav
ENTRYPOINT ["/sshnav"]
