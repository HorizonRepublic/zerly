import { HttpStatus } from '@nestjs/common';

import { ApiException } from '../exceptions/api.exception';

/**
 * Minimal shape matching class-validator's ValidationError.
 * Users must have class-validator installed to use this adapter.
 */
interface IValidationErrorShape {
  property: string;
  constraints?: Record<string, string>;
  children?: IValidationErrorShape[];
}

export const adaptClassValidatorErrors = (errors: IValidationErrorShape[]): ApiException =>
  new ApiException('VALIDATION_FAILED', {
    httpStatus: HttpStatus.UNPROCESSABLE_ENTITY,
    details: { errors: errors as unknown[] as Record<string, unknown>[] },
  });
