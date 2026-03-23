import { IErrorContext } from './error-context.interface';

export interface IErrorReporter {
  report(error: unknown, context: IErrorContext): void;
}
