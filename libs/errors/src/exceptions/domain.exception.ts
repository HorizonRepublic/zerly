import { HttpStatus } from '@nestjs/common';

import { IDomainExceptionOptions } from '../interfaces/domain-exception-options.interface';
import { IDomainExceptionPayload } from '../interfaces/domain-exception-payload.interface';

export abstract class DomainException extends Error {
  public readonly httpStatus: HttpStatus;
  public readonly details?: Record<string, unknown>;
  public readonly internalDetails?: Record<string, unknown>;

  public abstract readonly code: Uppercase<string>;

  protected constructor(options?: IDomainExceptionOptions) {
    super();
    this.name = this.constructor.name;
    this.httpStatus = options?.httpStatus ?? HttpStatus.BAD_REQUEST;
    if (options?.details !== undefined) this.details = options.details;
    if (options?.internalDetails !== undefined) this.internalDetails = options.internalDetails;
  }

  public toRpcPayload(): IDomainExceptionPayload {
    const payload: IDomainExceptionPayload = {
      isDomainException: true,
      code: this.code,
      httpStatus: this.httpStatus,
    };

    if (this.details !== undefined) payload.details = this.details;
    return payload;
  }
}
