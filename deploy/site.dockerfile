# ---- Build stage ----
# Build context is the repo root — see: docker build -f deploy/site.dockerfile -t site:v1 .
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Cache deps first
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build a static binary (CGO disabled so it runs on alpine with no libc issues)
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server .

# ---- Runtime stage ----
FROM alpine:3.20

# certs in case any subproject makes outbound HTTPS calls (Turnstile, Resend),
# tzdata since blog posts have dates
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Binary plus static assets and templates, baked in at build time. Blog
# posts/data and link-manager's uploaded files still change independently of
# the image, so those stay bind-mounted at runtime, see deploy/docker-compose.yml.
COPY --from=builder /app/server .
COPY --from=builder /app/static ./static
COPY --from=builder /app/templates ./templates

RUN mkdir -p blog/posts blog/data db/link-manager-filedata

EXPOSE 8080

ENTRYPOINT ["./server"]
