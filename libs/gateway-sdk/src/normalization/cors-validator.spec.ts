import { describe, expect, it } from '@jest/globals';

import { assertCorsCredentialsNotWildcard } from './cors-validator';

describe('assertCorsCredentialsNotWildcard', () => {
  it('accepts an explicit origin with credentials', () => {
    expect(() =>
      assertCorsCredentialsNotWildcard(
        { origins: ['https://app.example.com'], credentials: true },
        'test',
      ),
    ).not.toThrow();
  });

  it('accepts wildcard without credentials', () => {
    expect(() =>
      assertCorsCredentialsNotWildcard({ origins: ['*'], credentials: false }, 'test'),
    ).not.toThrow();
  });

  it('accepts wildcard with credentials omitted', () => {
    expect(() => assertCorsCredentialsNotWildcard({ origins: ['*'] }, 'test')).not.toThrow();
  });

  it('accepts an undefined cors config', () => {
    expect(() => assertCorsCredentialsNotWildcard(undefined, 'test')).not.toThrow();
  });

  it('rejects wildcard combined with credentials: true', () => {
    expect(() =>
      assertCorsCredentialsNotWildcard({ origins: ['*'], credentials: true }, 'test'),
    ).toThrow(/cannot be combined with cors.origins: '\*'/);
  });

  it('rejects wildcard mixed with explicit origins when credentials are on', () => {
    expect(() =>
      assertCorsCredentialsNotWildcard(
        { origins: ['https://app.example.com', '*'], credentials: true },
        'test',
      ),
    ).toThrow(/cannot be combined with cors.origins: '\*'/);
  });

  it('includes the context string in the error message', () => {
    expect(() =>
      assertCorsCredentialsNotWildcard(
        { origins: ['*'], credentials: true },
        '@GatewayRoute(POST /users)',
      ),
    ).toThrow(/@GatewayRoute\(POST \/users\)/);
  });
});
