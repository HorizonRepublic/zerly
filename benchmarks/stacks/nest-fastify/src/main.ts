// @ts-nocheck — bench stack has its own tsconfig + npm install happens inside docker
import 'reflect-metadata';

import { NestFactory } from '@nestjs/core';
import { FastifyAdapter, NestFastifyApplication } from '@nestjs/platform-fastify';

import { AppModule } from './app.module';

/**
 * Canonical NestJS + Fastify adapter bootstrap per the official
 * quick-start. No kernel, no bootstrap provider, no lifecycle
 * orchestration — everything lives in main.ts so the stack is easy
 * to review end-to-end as a single file.
 *
 * Logger is disabled (`logger: false` on both the FastifyAdapter and
 * the Nest application itself) because a per-request log line
 * dominates the hot path for a hello-world handler and would skew
 * the comparison against stacks that are also logger-free.
 *
 * Binds 0.0.0.0 inside the container so the host's docker-compose
 * port mapping can forward incoming traffic. The external port
 * mapping is configured by the top-level benchmarks/docker-compose.yml.
 */
async function bootstrap(): Promise<void> {
  const app = await NestFactory.create<NestFastifyApplication>(
    AppModule,
    new FastifyAdapter({ logger: false, disableRequestLogging: true }),
    { logger: false },
  );

  await app.listen(3000, '0.0.0.0');

  // eslint-disable-next-line no-console
  console.log('nest-fastify bench target listening on :3000');
}

bootstrap().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('bootstrap failed:', err);
  process.exit(1);
});
