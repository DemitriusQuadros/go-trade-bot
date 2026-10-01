# Stage 1: Build the React frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build the Go binary
FROM golang:1.25 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /web/dist/ ./cmd/api/webui/dist/

ARG TARGET

# Discovered building cmd/mcp on a low-memory host (observed: 2 CPU / ~2GB
# Docker VM): its dependency tree (Anthropic/Gemini SDKs, the MCP SDK) OOM-
# killed the Go compiler outright at default settings - -p=1 alone (cap
# build parallelism to one package at a time) was NOT enough; the single
# anthropic-sdk-go package's own compile still exceeded available memory.
# -gcflags=-l (disable inlining) is what actually fixed it - inlining is
# what blows up the compiler's memory footprint on large generated SDK
# code. Slower binary, not slower build in this case - trades a bit of
# runtime inlining for actually completing on a memory-constrained
# machine, which is the deployment target for this image (see
# deploy/README.md). Override with --build-arg GOFLAGS= (empty) when
# building somewhere with real headroom.
ARG GOFLAGS="-p=1 -gcflags=-l"
ENV GOFLAGS=${GOFLAGS}

RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/app ./cmd/$TARGET

# Stage 3: Runtime
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /bin/app .

CMD ["./app"]
