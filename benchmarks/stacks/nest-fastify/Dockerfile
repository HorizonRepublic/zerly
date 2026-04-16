# Three build targets sharing a single source tree:
#
#   - node-build   : tsc compile stage, only consumed by node-runtime.
#   - node-runtime : production-like Node 22 runtime running the compiled
#                    JS out of ./dist, no dev deps, no toolchain.
#   - bun-runtime  : oven/bun:1-alpine running the same TypeScript
#                    sources directly via `bun run` — no compile step,
#                    because bun executes TS natively with its own
#                    transpiler that honours experimentalDecorators +
#                    emitDecoratorMetadata from tsconfig.json.
#
# docker-compose picks which target to build via `target:` under the
# service's `build:` block. The source tree, package.json and
# tsconfig.json are completely runtime-agnostic — the only thing
# that changes between Node and Bun is the runtime container.
FROM node:22-alpine AS node-build
WORKDIR /app

COPY package.json ./
RUN npm install --no-audit --no-fund --no-progress

COPY tsconfig.json ./
COPY src ./src
RUN npx tsc

FROM node:22-alpine AS node-runtime
WORKDIR /app

COPY --from=node-build /app/node_modules ./node_modules
COPY --from=node-build /app/dist ./dist
COPY package.json ./

EXPOSE 3000
CMD ["node", "dist/main.js"]

FROM oven/bun:1-alpine AS bun-runtime
WORKDIR /app

COPY package.json ./
RUN bun install --no-save

COPY tsconfig.json ./
COPY src ./src

EXPOSE 3000
CMD ["bun", "run", "src/main.ts"]
