import 'reflect-metadata';

// Mock @nestjs/common, @nestjs/microservices, and @nestjs/microservices/constants
// to avoid ESM-only import failures under ts-jest. Computed-property keys
// match Nest's public API surface without triggering the camelCase
// `naming-convention` rule on shorthand method keys.
//
// The `MessagePattern` mock reproduces just the one behaviour this spec
// asserts on: defining `PATTERN_EXTRAS_METADATA` on `descriptor.value` with
// the supplied `extras` object. Transport metadata, pattern metadata, and
// the other framework-internal bookkeeping are intentionally omitted.
const PATTERN_EXTRAS_METADATA_KEY = 'microservices:pattern_extras';

jest.mock('@nestjs/microservices/constants', () => ({
  ['PATTERN_EXTRAS_METADATA']: PATTERN_EXTRAS_METADATA_KEY,
}));

jest.mock('@nestjs/core', () => ({
  ['Reflector']: class {
    public get = jest.fn();
  },
}));

jest.mock('@nestjs/common', () => ({
  ['applyDecorators']:
    (...decorators: readonly (MethodDecorator | ClassDecorator | PropertyDecorator)[]) =>
    (target: object, key: string | symbol, descriptor: PropertyDescriptor): PropertyDescriptor => {
      for (const decorator of decorators) {
        (decorator as MethodDecorator)(target, key, descriptor);
      }

      return descriptor;
    },
  ['UseInterceptors']: (): MethodDecorator => () => undefined,
  ['UseFilters']: (): MethodDecorator => () => undefined,
  ['Injectable']: (): ClassDecorator => (target) => target,
  ['Inject']: (): ParameterDecorator => () => undefined,
  ['Catch']: (): ClassDecorator => (target) => target,
}));

jest.mock('@nestjs/microservices', () => ({
  ['MessagePattern']:
    (_metadata: string, extras: Record<string, unknown>): MethodDecorator =>
    (_target, _propertyKey, descriptor) => {
      const value = (descriptor as PropertyDescriptor).value as object;

      Reflect.defineMetadata(PATTERN_EXTRAS_METADATA_KEY, extras, value);
      return descriptor;
    },
}));

import { ApiGateway } from './api-gateway.decorator';

describe('ApiGateway decorator', () => {
  class TestController {
    @ApiGateway({
      pattern: 'users.create',
      method: 'POST',
      path: '/users',
      statusCode: 201,
    })
    public createUser(): { id: number } {
      return { id: 1 };
    }

    @ApiGateway({ pattern: 'users.get', method: 'GET', path: '/users/:id' })
    public getUser(): { id: number } {
      return { id: 1 };
    }
  }

  it('writes http metadata to PATTERN_EXTRAS_METADATA with explicit statusCode', () => {
    const handler = TestController.prototype.createUser;
    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA_KEY, handler);

    expect(extras).toEqual({
      meta: {
        http: { method: 'POST', path: '/users', statusCode: 201 },
      },
    });
  });

  it('omits statusCode from http metadata when not provided', () => {
    const handler = TestController.prototype.getUser;
    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA_KEY, handler);

    expect(extras).toEqual({
      meta: {
        http: { method: 'GET', path: '/users/:id' },
      },
    });
    expect(extras.meta.http).not.toHaveProperty('statusCode');
  });
});
