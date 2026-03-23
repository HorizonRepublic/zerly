import { InjectionToken, ModuleMetadata } from '@nestjs/common';

import { IErrorReporter } from './error-reporter.interface';

export interface IErrorsModuleAsyncOptions extends Pick<ModuleMetadata, 'imports'> {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useFactory(...args: any[]): IErrorReporter | Promise<IErrorReporter>;
  inject?: InjectionToken[];
}
