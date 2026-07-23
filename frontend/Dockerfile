# ---- build ----
FROM node:20-bookworm-slim AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY . .
# Next.js serializes rewrite destinations at BUILD time, so BACKEND_URL must be
# set here (not just at runtime) for the /api proxy to target the backend service.
ENV NEXT_TELEMETRY_DISABLED=1 \
    BACKEND_URL=http://backend:8080
RUN npm run build

# ---- runtime ----
FROM node:20-bookworm-slim
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    BACKEND_URL=http://backend:8080
COPY --from=build /app/package.json /app/package-lock.json* ./
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/.next ./.next
COPY --from=build /app/next.config.js ./next.config.js
COPY --from=build /app/public ./public
EXPOSE 3000
CMD ["npm", "run", "start"]
