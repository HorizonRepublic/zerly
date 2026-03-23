import { HttpStatus } from '@nestjs/common';

export interface IDomainExceptionPayload {
  isDomainException: true;
  code: Uppercase<string>;
  httpStatus: HttpStatus;
  details?: Record<string, unknown>;
}
