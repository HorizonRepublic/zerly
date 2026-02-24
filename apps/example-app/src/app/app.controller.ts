import { Controller, Logger } from '@nestjs/common';

import { TypedRoute } from '@nestia/core';

import Get = TypedRoute.Get;

@Controller()
export class AppController {
  private readonly logger = new Logger(AppController.name);

  @Get()
  public getData(): number {
    return 5;
  }
}
