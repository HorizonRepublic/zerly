import {
  BadRequestException,
  HttpException,
  HttpStatus,
  NotFoundException,
  UnauthorizedException,
} from '@nestjs/common';

import { describe, expect, it } from '@jest/globals';

import { DefaultErrorBodyFactory } from './default-error-body.factory';

import type { IGatewayRequest } from '../types/gateway-request.interface';

describe('DefaultErrorBodyFactory', () => {
  const request: IGatewayRequest = {
    route: { method: 'GET', path: '/users/:id', matchedPath: '/users/1' },
    params: { id: '1' },
    query: {},
    headers: {},
    body: null,
    meta: {
      requestId: 'req-abc',
      remoteAddr: '127.0.0.1',
      receivedAt: 0,
      timeoutMs: 30000,
    },
  };

  describe('with a NestJS HttpException', () => {
    it('extracts status from NotFoundException via getStatus()', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(new NotFoundException('User 3 not found'), request);

      expect(result.status).toBe(HttpStatus.NOT_FOUND);
    });

    it('forwards Nest native body shape verbatim for NotFoundException', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(new NotFoundException('User 3 not found'), request);

      expect(result.body).toEqual({
        statusCode: HttpStatus.NOT_FOUND,
        message: 'User 3 not found',
        error: 'Not Found',
      });
    });

    it('forwards BadRequestException with array message (ValidationPipe shape)', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(
        new BadRequestException(['email must be an email', 'age must be a number']),
        request,
      );

      expect(result.status).toBe(HttpStatus.BAD_REQUEST);
      expect(result.body).toEqual({
        statusCode: HttpStatus.BAD_REQUEST,
        message: ['email must be an email', 'age must be a number'],
        error: 'Bad Request',
      });
    });

    it('forwards UnauthorizedException', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(new UnauthorizedException('token expired'), request);

      expect(result.status).toBe(HttpStatus.UNAUTHORIZED);
      expect(result.body).toEqual({
        statusCode: HttpStatus.UNAUTHORIZED,
        message: 'token expired',
        error: 'Unauthorized',
      });
    });

    it('wraps a plain-string HttpException response into { statusCode, message }', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(new HttpException('raw string body', 418), request);

      expect(result.status).toBe(418);
      expect(result.body).toEqual({
        statusCode: 418,
        message: 'raw string body',
      });
    });

    it('forwards custom subclass structured response verbatim', () => {
      // Custom HttpException subclasses that pass a structured object to
      // the base constructor should round-trip every field untouched — the
      // factory never normalizes, re-keys, or strips anything. This is the
      // extensibility path users take when they want a richer error shape
      // without writing a full IErrorBodyFactory.
      class TeapotException extends HttpException {
        public constructor() {
          super(
            {
              statusCode: 418,
              message: 'short and stout',
              error: "I'm a Teapot",
              teapotId: 'tp-42',
              brew: 'earl-grey',
            },
            418,
          );
        }
      }

      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(new TeapotException(), request);

      expect(result.status).toBe(418);
      expect(result.body).toEqual({
        statusCode: 418,
        message: 'short and stout',
        error: "I'm a Teapot",
        teapotId: 'tp-42',
        brew: 'earl-grey',
      });
    });
  });

  describe('with an unrecognized throw', () => {
    it('returns a generic 500 body for a plain Error', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(new Error('raw internal detail'), request);

      expect(result.status).toBe(HttpStatus.INTERNAL_SERVER_ERROR);
      expect(result.body).toEqual({
        statusCode: HttpStatus.INTERNAL_SERVER_ERROR,
        message: 'Internal server error',
      });
    });

    it('never leaks the original Error message into the body', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(
        new Error('postgres connection to db.internal:5432 failed'),
        request,
      );

      expect(JSON.stringify(result.body)).not.toContain('postgres');
      expect(JSON.stringify(result.body)).not.toContain('db.internal');
    });

    it('handles string throws with the same generic 500 body', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build('some string', request);

      expect(result.status).toBe(HttpStatus.INTERNAL_SERVER_ERROR);
      expect(result.body).toEqual({
        statusCode: HttpStatus.INTERNAL_SERVER_ERROR,
        message: 'Internal server error',
      });
    });

    it('handles null throws with the same generic 500 body', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(null, request);

      expect(result.status).toBe(HttpStatus.INTERNAL_SERVER_ERROR);
      expect(result.body).toEqual({
        statusCode: HttpStatus.INTERNAL_SERVER_ERROR,
        message: 'Internal server error',
      });
    });

    it('handles number throws with the same generic 500 body', () => {
      const factory = new DefaultErrorBodyFactory();
      const result = factory.build(42, request);

      expect(result.status).toBe(HttpStatus.INTERNAL_SERVER_ERROR);
    });
  });
});
