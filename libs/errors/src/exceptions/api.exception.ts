import { IDomainExceptionOptions } from '../interfaces/domain-exception-options.interface';
import { IDomainExceptionPayload } from '../interfaces/domain-exception-payload.interface';

import { DomainException } from './domain.exception';

export class ApiException extends DomainException {
  public constructor(
    public override readonly code: Uppercase<string>,
    options?: IDomainExceptionOptions,
  ) {
    super(options);
  }

  public static fromRpcPayload(payload: unknown): ApiException | null {
    if (!isDomainExceptionPayload(payload)) return null;

    return new ApiException(payload.code, {
      httpStatus: payload.httpStatus,
      ...(payload.details !== undefined && { details: payload.details }),
    });
  }
}

const isDomainExceptionPayload = (payload: unknown): payload is IDomainExceptionPayload =>
  typeof payload === 'object' &&
  payload !== null &&
  'isDomainException' in payload &&
  (payload as Record<string, unknown>)['isDomainException'] === true;
