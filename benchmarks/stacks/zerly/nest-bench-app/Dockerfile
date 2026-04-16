# Three build targets sharing a single source tree (see sibling
# nest-fastify / nest-express Dockerfiles for the generic rationale).
#
# Zerly-specific twist: both runtime stages rely on `vendor/gateway-sdk`
# being pre-built on the host and copied here by run-all.sh before the
# image build. The package.json workspace link
# `"@zerly/gateway-sdk": "file:./vendor/gateway-sdk"` resolves against
# that COPY-in'd directory, so neither npm nor bun has any reason to
# talk to the registry about it.
FROM node:22-alpine AS node-build
WORKDIR /app

COPY package.json ./
COPY vendor ./vendor
RUN npm install --no-audit --no-fund --no-progress

COPY tsconfig.json ./
COPY src ./src
RUN npx tsc

FROM node:22-alpine AS node-runtime
WORKDIR /app

COPY --from=node-build /app/node_modules ./node_modules
COPY --from=node-build /app/dist ./dist
COPY --from=node-build /app/vendor ./vendor
COPY package.json ./

EXPOSE 3000
CMD ["node", "dist/main.js"]

FROM oven/bun:1-alpine AS bun-runtime
WORKDIR /app

COPY package.json ./
COPY vendor ./vendor
RUN bun install --no-save

COPY tsconfig.json ./
COPY src ./src

EXPOSE 3000
CMD ["bun", "run", "src/main.ts"]
