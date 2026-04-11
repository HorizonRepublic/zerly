const express = require('express');
const http = require('node:http');

/*
 * Express front that proxies GET /demo/users/1 to a separate Node
 * upstream process via Node's stdlib http.request.
 *
 * Design notes:
 *   - No axios, no node-fetch, no undici. The point of this scenario
 *     is to measure "Express + Node stdlib", which is what you get by
 *     default in a vanilla Express project. Pulling in a third-party
 *     HTTP client would measure something else and make the
 *     comparison less meaningful.
 *   - The http.Agent runs with keepAlive: true and a generous
 *     maxSockets value. Running without keep-alive would open a fresh
 *     TCP connection per upstream call, which is a production
 *     antipattern nobody would ship — so disabling it would penalize
 *     Express for a misconfiguration we would never suggest.
 *   - x-powered-by and etag are disabled for the same fairness
 *     reasons as express-hello.
 *   - The upstream response is piped straight into the Express
 *     response to avoid materializing the body in user-land.
 *   - Bound to 127.0.0.1 only.
 */

const PORT = Number(process.env.PORT ?? 18084);
const UPSTREAM_HOST = process.env.UPSTREAM_HOST ?? '127.0.0.1';
const UPSTREAM_PORT = Number(process.env.UPSTREAM_PORT ?? 18090);

const agent = new http.Agent({ keepAlive: true, maxSockets: 128 });

const app = express();
app.disable('x-powered-by');
app.disable('etag');

app.get('/demo/users/1', (_req, res) => {
  const upstreamReq = http.request(
    {
      host: UPSTREAM_HOST,
      port: UPSTREAM_PORT,
      path: '/users/1',
      method: 'GET',
      agent,
    },
    (upstreamRes) => {
      res.status(upstreamRes.statusCode ?? 502).type('application/json');
      upstreamRes.pipe(res);
    },
  );
  upstreamReq.on('error', (err) => {
    res.status(502).json({ error: 'upstream_error', message: err.message });
  });
  upstreamReq.end();
});

app.listen(PORT, '127.0.0.1', () => {
  process.stdout.write(`express-proxy listening on ${PORT} -> ${UPSTREAM_HOST}:${UPSTREAM_PORT}\n`);
});
