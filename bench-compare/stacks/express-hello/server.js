const express = require('express');

/*
 * Minimal Express hello-world used as a baseline for throughput comparison.
 *
 * Design notes:
 *   - CommonJS on purpose. Express's documentation and the overwhelming
 *     majority of real-world Express deployments are CJS; switching to ESM
 *     would measure a setup nobody actually ships.
 *   - `x-powered-by` and `etag` are both disabled. Fastify does not set
 *     either by default, so leaving them on would penalize Express for
 *     work the comparison stack simply does not do. The fair
 *     apples-to-apples configuration turns both off.
 *   - Bound to 127.0.0.1 to keep the measurement on pure loopback.
 */

const PORT = Number(process.env.PORT ?? 18082);

const app = express();
app.disable('x-powered-by');
app.disable('etag');

app.get('/demo/users/1', (_req, res) => {
  res.json({ id: '1', name: 'Alice' });
});

app.listen(PORT, '127.0.0.1', () => {
  process.stdout.write(`express-hello listening on ${PORT}\n`);
});
