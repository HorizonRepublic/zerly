import { IncomingMessage } from 'node:http';

import { v7 } from 'uuid';

import { HeaderKeys } from '../enum/header-keys.enum';

export const genReqId = (req: IncomingMessage): string => {
  const headers = req.headers;
  const id = headers[HeaderKeys.TraceId] ?? headers[HeaderKeys.CorrelationId];

  return (Array.isArray(id) ? id[0] : id) ?? v7();
};
