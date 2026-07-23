# ---- build ----
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/server ./cmd

# ---- runtime (needs Chromium for chromedp) ----
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      chromium \
      ca-certificates \
      fonts-liberation \
      tini \
  && rm -rf /var/lib/apt/lists/*

ENV CHROME_BIN=/usr/bin/chromium \
    DATA_DIR=/data \
    PORT=8080 \
    HEADLESS=true \
    GIN_MODE=release

COPY --from=build /out/server /usr/local/bin/server

EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["server"]
