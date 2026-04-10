import { INestApplication, Inject, Injectable } from '@nestjs/common';

import { JetstreamStrategy } from '@horizon-republic/nestjs-jetstream';
import { from, map, Observable } from 'rxjs';

import { APP_STATE_SERVICE, IAppStateService } from '@zerly/kernel';

import { MICROSERVICE_OPTIONS } from '../const';
import { IMicroserviceModuleOptions } from '../types/microservice-module.options';

/**
 * Lifecycle-aware provider that attaches the JetStream transport strategy to
 * the host NestJS HTTP application.
 *
 * Subscribes to the kernel's `APP_STATE_SERVICE.onListening` callback so the
 * microservice transport is connected only after the main HTTP adapter has
 * finished bootstrapping. This enables hybrid HTTP + NATS JetStream
 * applications without forcing consumers to juggle `connectMicroservice` /
 * `startAllMicroservices` calls in their `main.ts`.
 *
 * The strategy instance is resolved from the DI container using a non-strict
 * lookup because `JetstreamModule` registers it at the global scope.
 */
@Injectable()
export class MicroserviceServerProvider {
  public constructor(
    @Inject(APP_STATE_SERVICE)
    private readonly stateService: IAppStateService,
    @Inject(MICROSERVICE_OPTIONS)
    private readonly options: IMicroserviceModuleOptions,
  ) {
    this.stateService.onListening((app: INestApplication): Observable<void> => {
      return this.serveMicroservice(app);
    });
  }

  /**
   * Connect and start the JetStream microservice transport on the given app.
   *
   * Retrieves the `JetstreamStrategy` provider from the global scope, wraps it
   * in the shape NestJS's `connectMicroservice` expects (`{ strategy }`), and
   * triggers `startAllMicroservices` inside an RxJS observable so callers can
   * compose it with other startup steps managed by the kernel.
   * @param app The running Nest application into which the microservice
   * transport should be mounted.
   * @returns Observable that completes once all microservices have started.
   */
  private serveMicroservice(app: INestApplication): Observable<void> {
    const strategy = app.get(JetstreamStrategy, { strict: false });

    app.connectMicroservice({ strategy }, { inheritAppConfig: true });

    return from(app.startAllMicroservices()).pipe(map(() => void 0));
  }
}
