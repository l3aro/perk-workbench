# syntax=docker/dockerfile:1
FROM node:22-alpine AS frontend-build
WORKDIR /src
COPY package.json package-lock.json vite.config.mjs ./
COPY frontend/ ./frontend/
# site.css scans templates via `@source "../internal/site/templates"` so
# Tailwind can discover utilities used only inside Go-rendered pages.
COPY internal/site/templates ./internal/site/templates
RUN npm ci --include=optional && npm run build

FROM golang:1.27-alpine AS site-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY protocol/ ./protocol/
# The frontend bundles are generated (not committed); produce them in a Node
# stage and hand the dist into the Go build so go:embed sees them.
COPY --from=frontend-build /src/internal/site/assets/dist ./internal/site/assets/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/perk-workbench ./cmd/perk-workbench \
  && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/perk-workbench-site ./cmd/perk-workbench-site

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
COPY --from=site-build /out/perk-workbench-site /usr/local/bin/perk-workbench-site
COPY --from=site-build /out/perk-workbench /usr/local/bin/perk-workbench
EXPOSE 8080
ENV PORT=8080
USER app
ENTRYPOINT ["/usr/local/bin/perk-workbench-site"]