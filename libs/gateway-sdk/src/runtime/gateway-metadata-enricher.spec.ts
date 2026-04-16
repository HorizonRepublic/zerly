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
});
