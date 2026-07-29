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
      # Built-in display for the sign-in window: an X server nobody has to
      # provide, exported to a browser tab over VNC/noVNC.
      xvfb \
      x11vnc \
      novnc \
      websockify \
  && rm -rf /var/lib/apt/lists/*

ENV CHROME_BIN=/usr/bin/chromium \
    DATA_DIR=/data \
    PORT=8080 \
    HEADLESS=false \
    DISPLAY=:99 \
    NOVNC_PORT=7900 \
    GIN_MODE=release

COPY --from=build /out/server /usr/local/bin/server
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# 8080 = API, 7900 = the sign-in window in your browser.
EXPOSE 8080 7900
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/docker-entrypoint.sh"]
CMD ["server"]
