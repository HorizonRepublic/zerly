import { HttpStatus } from '@nestjs/common';

import { TypeGuardError } from 'typia';

import { ApiException } from '../exceptions/api.exception';

export const adaptTypiaError = (error: unknown): ApiException | null => {
  if (!(error instanceof TypeGuardError)) return null;

  return new ApiException('VALIDATION_FAILED', {
    httpStatus: HttpStatus.UNPROCESSABLE_ENTITY,
    details: {
      path: error.path,
      expected: error.expected,
      value: error.value,
    },
  });
};
