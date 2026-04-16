// @ts-nocheck — bench stack has its own tsconfig + npm install happens inside docker
import 'reflect-metadata';

import { NestFactory } from '@nestjs/core';
import { ExpressAdapter, NestExpressApplication } from '@nestjs/platform-express';

import { AppModule } from './app.module';

/**
 * Canonical NestJS + Express adapter bootstrap per the official
 * quick-start. No kernel, no bootstrap provider, no lifecycle
 * orchestration — everything lives in main.ts so the stack is easy
 * to review end-to-end as a single file.
 *
 * `ExpressAdapter` has no logger knob of its own (Express itself is
 * silent by default) so passing `{ logger: false }` at the Nest app
 * level is enough to mirror the nest-fastify setup: no per-request
 * log lines, same hello-world hot path.
 *
 * Binds 0.0.0.0 inside the container so the host's docker-compose
 * port mapping can forward incoming traffic.
 */
async function bootstrap(): Promise<void> {
  const app = await NestFactory.create<NestExpressApplication>(
    AppModule,
    new ExpressAdapter(),
    { logger: false },
  );

  await app.listen(3000, '0.0.0.0');

  // eslint-disable-next-line no-console
  console.log('nest-express bench target listening on :3000');
}

bootstrap().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('bootstrap failed:', err);
  process.exit(1);
});
