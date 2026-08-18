# syntax=docker/dockerfile:1
FROM golang:1.26.6-alpine AS tui-build
WORKDIR /src
COPY source/go.mod source/go.sum ./
RUN go mod download
COPY source/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/perk-workbench ./cmd/perk-workbench

FROM golang:1.26.6-alpine AS site-build
WORKDIR /src
COPY website/go.mod website/go.sum ./
RUN go mod download
COPY website/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/perk-workbench-site ./cmd/perk-workbench-site

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
COPY --from=tui-build /out/perk-workbench /usr/local/bin/perk-workbench
COPY --from=site-build /out/perk-workbench-site /usr/local/bin/perk-workbench-site
USER app
EXPOSE 8080
ENV PORT=8080
ENTRYPOINT ["/usr/local/bin/perk-workbench-site"]
