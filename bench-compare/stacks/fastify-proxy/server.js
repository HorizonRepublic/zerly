import Fastify from 'fastify';
import { request } from 'undici';

/*
 * Fastify front that proxies GET /demo/users/1 to a separate Node
 * upstream process via undici.request.
 *
 * Design notes:
 *   - undici is the HTTP client used internally by Fastify's own
 *     @fastify/http-proxy plugin and is the fastest general-purpose
 *     Node HTTP client. Picking it matches what a competent Fastify
 *     deployment would ship.
 *   - The handler returns `upstream.body`, which is a readable stream.
 *     Fastify pipes the stream straight to the outgoing response, so
 *     the response body never gets materialized into a string in
 *     user-land — this is the zero-copy path and is the fair
 *     comparison point for a proxy.
 *   - Logger off, request logging off, bound to 127.0.0.1 only, for
 *     the same reasons as the hello scenarios.
 */

const PORT = Number(process.env.PORT ?? 18083);
const UPSTREAM = process.env.UPSTREAM ?? 'http://127.0.0.1:18090';

const app = Fastify({
  logger: false,
  disableRequestLogging: true,
});

app.get('/demo/users/1', async (_req, reply) => {
  const upstream = await request(`${UPSTREAM}/users/1`);
  reply.code(upstream.statusCode).header('content-type', 'application/json');
  return upstream.body;
});

try {
  await app.listen({ port: PORT, host: '127.0.0.1' });
  process.stdout.write(`fastify-proxy listening on ${PORT} -> ${UPSTREAM}\n`);
} catch (err) {
  process.stderr.write(`fastify-proxy failed: ${String(err)}\n`);
  process.exit(1);
}
