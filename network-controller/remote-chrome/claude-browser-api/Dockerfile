FROM node:22-bookworm-slim

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts --no-audit --no-fund
COPY server.js ./server.js

ENV NODE_ENV=production
ENV API_MODEL=claude-browser
ENV API_HOST=10.2.0.12
ENV API_PORT=9186
ENV CDP_URL=http://127.0.0.1:9222

USER node
EXPOSE 9186
CMD ["node", "server.js"]
