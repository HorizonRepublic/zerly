import Fastify from 'fastify';

/*
 * Minimal Fastify hello-world used as a baseline for throughput comparison.
 *
 * Design notes:
 *   - logger is disabled and disableRequestLogging is true because any
 *     per-request log line dominates the hot path for a trivial handler
 *     and would skew the comparison with stacks that are also logger-free.
 *   - We bind 127.0.0.1 explicitly rather than 0.0.0.0 to measure pure
 *     loopback, avoiding an extra IPv6 resolution hop that some kernels
 *     add when binding to the wildcard address.
 */

const PORT = Number(process.env.PORT ?? 18081);

const app = Fastify({
  logger: false,
  disableRequestLogging: true,
});

app.get('/demo/users/1', async () => ({ id: '1', name: 'Alice' }));

try {
  await app.listen({ port: PORT, host: '127.0.0.1' });
  process.stdout.write(`fastify-hello listening on ${PORT}\n`);
} catch (err) {
  process.stderr.write(`fastify-hello failed: ${String(err)}\n`);
  process.exit(1);
}
