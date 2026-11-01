FROM node:26.13.0-alpine3.23@sha256:e6c03d42b46af3b1e98207e59f06a43cfe5efd3ec4b4d3d9802bddf04f833374 AS builder

WORKDIR /app

COPY package*.json ./

RUN npm ci --ignore-scripts

COPY . .

RUN npm run build

FROM node:26.13.0-alpine3.23@sha256:e6c03d42b46af3b1e98207e59f06a43cfe5efd3ec4b4d3d9802bddf04f833374 AS runner

WORKDIR /app

RUN addgroup -g 1001 -S nodejs && \
    adduser -S nodejs -u 1001

COPY package*.json ./

RUN npm ci --omit=dev --ignore-scripts && \
    npm cache clean --force

COPY --from=builder --chown=nodejs:nodejs /app/dist ./dist

RUN mkdir -p ./dist/runtime && \
    chown -R nodejs:nodejs ./dist/runtime && \
    chmod 700 ./dist/runtime

ENV NODE_ENV=production \
    HAECHI_SITE_HOST=0.0.0.0 \
    HAECHI_SITE_PORT=4173

USER nodejs

EXPOSE 4173

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD node -e "require('http').get('http://localhost:4173/api/health', (res) => { process.exit(res.statusCode === 200 ? 0 : 1) }).on('error', () => process.exit(1))"

CMD ["node", "dist/server.mjs"]
