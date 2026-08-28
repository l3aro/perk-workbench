# syntax=docker/dockerfile:1
FROM node:22-alpine AS frontend-build
WORKDIR /src
COPY website/package.json website/package-lock.json ./
RUN npm ci
COPY website/ ./
RUN npm run build

FROM node:22-alpine AS tui-build
# The demo TUI comes from the official npm release, pinned to 0.10.1.
# npm installs a JS launcher plus the native binary as a nested optional
# dependency; only the native binary ships into the final Alpine image.
RUN npm install -g --prefix /tui perk-workbench@0.10.1 \
  && find /tui/lib/node_modules/perk-workbench/node_modules \
  -path '*/bin/perk-workbench' -type f \
  -exec cp {} /tui/perk-workbench \;

FROM golang:1.26.6-alpine AS site-build
WORKDIR /src
COPY website/go.mod website/go.sum ./
RUN go mod download
COPY website/ ./
# The frontend bundles are generated (not committed); produce them in a Node
# stage and hand the dist into the Go build so go:embed sees them.
COPY --from=frontend-build /src/internal/site/assets/dist ./internal/site/assets/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/perk-workbench-site ./cmd/perk-workbench-site

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
COPY --from=site-build /out/perk-workbench-site /usr/local/bin/perk-workbench-site
COPY --from=tui-build /tui/perk-workbench /usr/local/bin/perk-workbench
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/usr/local/bin/perk-workbench-site"]
