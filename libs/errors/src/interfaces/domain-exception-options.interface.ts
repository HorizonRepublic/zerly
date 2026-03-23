import { HttpStatus } from '@nestjs/common';

export interface IDomainExceptionOptions {
  httpStatus?: HttpStatus;
  details?: Record<string, unknown>;
  internalDetails?: Record<string, unknown>;
}
