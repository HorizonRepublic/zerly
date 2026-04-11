import Fastify from 'fastify';

/*
 * Dedicated upstream for the proxy scenarios. It is intentionally a
 * separate process (not an in-process handler) so that the proxy
 * scenarios measure actual cross-process HTTP round-trips on loopback,
 * which is what a real deployment looks like.
 *
 * Design notes mirror fastify-hello: logger off, request logging off,
 * bound to 127.0.0.1 only. The public path here is `/users/1`, not
 * `/demo/users/1`, because the proxy scenarios rewrite the path on
 * forward.
 */

const PORT = Number(process.env.PORT ?? 18090);

const app = Fastify({
  logger: false,
  disableRequestLogging: true,
});

app.get('/users/1', async () => ({ id: '1', name: 'Alice' }));

try {
  await app.listen({ port: PORT, host: '127.0.0.1' });
  process.stdout.write(`upstream-node listening on ${PORT}\n`);
} catch (err) {
  process.stderr.write(`upstream-node failed: ${String(err)}\n`);
  process.exit(1);
}
