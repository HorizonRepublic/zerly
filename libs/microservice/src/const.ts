import { ServiceToken } from '@zerly/core';

export const MICROSERVICE_SERVER_PROVIDER = Symbol(
  'MICROSERVICE_SERVER_PROVIDER',
) as ServiceToken<'microservice'>;

export const MICROSERVICE_CLIENT_PROVIDER = Symbol(
  'MICROSERVICE_CLIENT_PROVIDER',
) as ServiceToken<'microservice'>;

export const MICROSERVICE_OPTIONS = Symbol('MICROSERVICE_OPTIONS') as ServiceToken<'microservice'>;
