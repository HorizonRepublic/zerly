import { Injectable, Logger } from '@nestjs/common';
import { RuntimeException } from '@nestjs/core/errors/exceptions';
import { NestFastifyApplication } from '@nestjs/platform-fastify';

import { IAppRefService } from '../types';

@Injectable()
export class AppRefService implements IAppRefService {
  protected appRef: NestFastifyApplication | null = null;
  protected readonly logger = new Logger(AppRefService.name);

  public get(): NestFastifyApplication {
    if (!this.appRef) {
      throw new RuntimeException(
        `AppRefService.getApp() has not been called yet. Ensure that you trying to get in .onCreated() state`,
      );
    }

    return this.appRef;
  }

  public set(app: NestFastifyApplication): this {
    if (this.appRef) {
      this.logger.warn(`AppRefService.setApp() has already been called.`);

      return this;
    }

    this.appRef = app;

    return this;
  }
}
