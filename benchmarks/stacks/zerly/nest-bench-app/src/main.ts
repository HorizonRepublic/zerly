// @ts-nocheck — bench stack has its own tsconfig + npm install happens inside docker
import 'reflect-metadata';

import { NestFactory } from '@nestjs/core';
import { FastifyAdapter, NestFastifyApplication } from '@nestjs/platform-fastify';

import { JetstreamStrategy } from '@horizon-republic/nestjs-jetstream';

import { AppModule } from './app.module';

/**
 * Canonical two-phase bootstrap for a Nest + JetStream + Gateway SDK
 * application, inlined in main.ts per the Nest quick-start style.
 *
 * Why two phases rather than `NestFactory.createMicroservice`: the
 * `JetstreamStrategy` class has an 11-argument DI-injected
 * constructor (ConnectionProvider, PatternRegistry, StreamProvider,
 * ConsumerProvider, MessageProvider, EventRouter, RpcRouter,
 * CoreRpcServer, ackWaitMap, MetadataProvider). It *cannot* be
 * instantiated directly — it must be resolved from the container
 * that `JetstreamModule.forRoot(...)` built. So:
 *
 *   1. Build a Nest application (we use a Fastify adapter purely for
 *      the health endpoint + the `/` 404 the compose healthcheck
 *      pings). `logger: false` everywhere to keep the hot path free
 *      of per-request I/O.
 *
 *   2. Resolve the strategy from the DI graph with `app.get(...)`
 *      and `strict: false` so the lookup walks global modules.
 *
 *   3. Call `connectMicroservice({ strategy })` + `startAllMicroservices()`
 *      so the JetStream subscribe side is wired before any HTTP
 *      traffic can arrive.
 *
 *   4. Finally bind the HTTP adapter on port 3000. The bench-app's
 *      HTTP port is *not* exposed in docker-compose — the gateway
 *      server is the only externally reachable thing — but the
 *      healthcheck needs something to hit.
 */
async function bootstrap(): Promise<void> {
  const app = await NestFactory.create<NestFastifyApplication>(
    AppModule,
    new FastifyAdapter({ logger: false, disableRequestLogging: true }),
    { logger: false },
  );

  const strategy = app.get(JetstreamStrategy, { strict: false });
  app.connectMicroservice({ strategy }, { inheritAppConfig: true });
  await app.startAllMicroservices();

  await app.listen(3000, '0.0.0.0');

  // eslint-disable-next-line no-console
  console.log('zerly bench-app listening on :3000 (HTTP) + JetStream attached');
}

bootstrap().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('bootstrap failed:', err);
  process.exit(1);
});
