import 'reflect-metadata';

import { Controller } from '@nestjs/common';
import { DiscoveryModule } from '@nestjs/core';
import { PATTERN_EXTRAS_METADATA } from '@nestjs/microservices/constants';
import { Test } from '@nestjs/testing';

import { beforeEach, describe, expect, it } from '@jest/globals';

import { GatewayRoute } from '../decorators/gateway-route.decorator';
import { DefaultErrorBodyFactory } from '../normalization/default-error-body.factory';
import { DefaultGatewayReplyBuilder } from '../normalization/default-reply.builder';
import { DefaultStatusResolver } from '../normalization/default-status.resolver';
import {
  GATEWAY_DEFAULTS,
  GATEWAY_ERROR_BODY_FACTORY,
  GATEWAY_REPLY_BUILDER,
  GATEWAY_STATUS_RESOLVER,
} from '../tokens';

import { GatewayMetadataEnricher } from './gateway-metadata-enricher';

import type { IGatewayDefaults } from '../types';

@Controller()
class StubController {
  @GatewayRoute({
    pattern: 'test.hello',
    method: 'GET',
    path: '/hello',
  })
  public hello(): void {}

  @GatewayRoute({
    pattern: 'test.other',
    method: 'POST',
    path: '/other',
    cors: { origins: ['https://override.com'] },
    headers: { 'cache-control': 'no-store' },
  })
  public other(): void {}
}

@Controller()
class ControllerA {
  @GatewayRoute({ pattern: 'a.hello', method: 'GET', path: '/a/hello' })
  public hello(): void {}
}

@Controller()
class ControllerB {
  @GatewayRoute({ pattern: 'b.hello', method: 'GET', path: '/b/hello' })
  public hello(): void {}
}

@Controller()
class RateLimitController {
  @GatewayRoute({ pattern: 'rl.hello', method: 'GET', path: '/rl/hello' })
  public hello(): void {}
}

describe('GatewayMetadataEnricher', () => {
  const defaults: IGatewayDefaults = {
    cors: { origins: ['https://global.com'] },
    headers: { 'x-frame-options': 'DENY' },
    timeout: 30_000,
  };

  let sut: GatewayMetadataEnricher;

  beforeEach(async () => {
    const module = await Test.createTestingModule({
      imports: [DiscoveryModule],
      controllers: [StubController],
      providers: [
        GatewayMetadataEnricher,
        { provide: GATEWAY_DEFAULTS, useValue: Object.freeze(defaults) },
        { provide: GATEWAY_REPLY_BUILDER, useClass: DefaultGatewayReplyBuilder },
        { provide: GATEWAY_STATUS_RESOLVER, useClass: DefaultStatusResolver },
        { provide: GATEWAY_ERROR_BODY_FACTORY, useClass: DefaultErrorBodyFactory },
      ],
    }).compile();

    sut = module.get(GatewayMetadataEnricher);
  });

  it('should merge defaults into route metadata that has no overrides', () => {
    sut.onModuleInit();

    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, StubController.prototype.hello) as {
      meta: Record<string, unknown>;
    };

    expect(extras.meta['cors']).toEqual({ origins: ['https://global.com'] });
    expect(extras.meta['headers']).toEqual({ 'x-frame-options': 'DENY' });
    expect(extras.meta['timeout']).toBe(30_000);
  });

  it('should shallow-replace cors when route has its own override', () => {
    sut.onModuleInit();

    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, StubController.prototype.other) as {
      meta: Record<string, unknown>;
    };

    expect(extras.meta['cors']).toEqual({ origins: ['https://override.com'] });
  });

  it('should deep-merge headers from defaults and route', () => {
    sut.onModuleInit();

    const extras = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, StubController.prototype.other) as {
      meta: Record<string, unknown>;
    };

    expect(extras.meta['headers']).toEqual({
      'x-frame-options': 'DENY',
      'cache-control': 'no-store',
    });
  });

  describe('multi-controller enrichment', () => {
    let multiSut: GatewayMetadataEnricher;

    beforeEach(async () => {
      const module = await Test.createTestingModule({
        imports: [DiscoveryModule],
        controllers: [ControllerA, ControllerB],
        providers: [
          GatewayMetadataEnricher,
          { provide: GATEWAY_DEFAULTS, useValue: Object.freeze(defaults) },
          { provide: GATEWAY_REPLY_BUILDER, useClass: DefaultGatewayReplyBuilder },
          { provide: GATEWAY_STATUS_RESOLVER, useClass: DefaultStatusResolver },
          { provide: GATEWAY_ERROR_BODY_FACTORY, useClass: DefaultErrorBodyFactory },
        ],
      }).compile();

      multiSut = module.get(GatewayMetadataEnricher);
    });

    it('should enrich handlers across multiple controllers', () => {
      multiSut.onModuleInit();

      const extrasA = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, ControllerA.prototype.hello) as {
        meta: Record<string, unknown>;
      };

      const extrasB = Reflect.getMetadata(PATTERN_EXTRAS_METADATA, ControllerB.prototype.hello) as {
        meta: Record<string, unknown>;
      };

      expect(extrasA.meta['cors']).toEqual({ origins: ['https://global.com'] });
      expect(extrasA.meta['timeout']).toBe(30_000);
      expect(extrasA.meta['headers']).toEqual({ 'x-frame-options': 'DENY' });

      expect(extrasB.meta['cors']).toEqual({ origins: ['https://global.com'] });
      expect(extrasB.meta['timeout']).toBe(30_000);
      expect(extrasB.meta['headers']).toEqual({ 'x-frame-options': 'DENY' });
    });
  });

  describe('rateLimit defaults', () => {
    it('should merge rateLimit defaults into route metadata', async () => {
      const rateLimitDefaults: IGatewayDefaults = {
        rateLimit: { rps: 100, keyBy: ['ip'] },
        timeout: 5000,
      };

      const module = await Test.createTestingModule({
        imports: [DiscoveryModule],
        controllers: [RateLimitController],
        providers: [
          GatewayMetadataEnricher,
          { provide: GATEWAY_DEFAULTS, useValue: Object.freeze(rateLimitDefaults) },
          { provide: GATEWAY_REPLY_BUILDER, useClass: DefaultGatewayReplyBuilder },
          { provide: GATEWAY_STATUS_RESOLVER, useClass: DefaultStatusResolver },
          { provide: GATEWAY_ERROR_BODY_FACTORY, useClass: DefaultErrorBodyFactory },
        ],
      }).compile();

      const rateLimitSut = module.get(GatewayMetadataEnricher);

      rateLimitSut.onModuleInit();

      const extras = Reflect.getMetadata(
        PATTERN_EXTRAS_METADATA,
        RateLimitController.prototype.hello,
      ) as { meta: Record<string, unknown> };

      expect(extras.meta['rateLimit']).toEqual({ rps: 100, keyBy: ['ip'] });
      expect(extras.meta['timeout']).toBe(5000);
    });
  });
});
