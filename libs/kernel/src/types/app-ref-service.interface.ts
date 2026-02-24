import { NestFastifyApplication } from '@nestjs/platform-fastify';

export interface IAppRefService {
  get(): NestFastifyApplication;

  set(app: NestFastifyApplication): this;
}
