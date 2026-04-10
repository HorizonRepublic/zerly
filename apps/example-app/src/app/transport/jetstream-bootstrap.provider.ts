import { Inject, Injectable } from '@nestjs/common';

import { JetstreamStrategy } from '@horizon-republic/nestjs-jetstream';
import { from, map, type Observable } from 'rxjs';

import { APP_STATE_SERVICE, type IAppStateService } from '@zerly/kernel';

import type { NestFastifyApplication } from '@nestjs/platform-fastify';

/**
 * Lifecycle-aware provider that attaches the JetStream transport strategy to
 * the example-app's NestJS application without requiring any change to
 * `main.ts`.
 * @remarks
 * Registration happens through the kernel's `APP_STATE_SERVICE.onListening`
 * hook — this is the canonical extension point for plugging microservice and
 * websocket transports into a `@zerly/kernel`-based application. Using the
 * hook keeps `main.ts` a single line (`Kernel.init(...).subscribe()`) and
 * concentrates bootstrap coupling inside provider graphs that are naturally
 * reviewable alongside their feature modules.
 *
 * The provider is listed in the root application module's `providers` array
 * so NestJS instantiates it during module initialization. The constructor
 * subscribes to `onListening`; the callback fires once the HTTP adapter has
 * finished bootstrapping and the app transitions into the `Listening` state,
 * at which point the JetStream strategy is resolved from the global DI scope
 * and wired via `app.connectMicroservice` + `app.startAllMicroservices`.
 */
@Injectable()
export class JetstreamBootstrapProvider {
  public constructor(
    @Inject(APP_STATE_SERVICE)
    private readonly stateService: IAppStateService,
  ) {
    this.stateService.onListening((app: NestFastifyApplication): Observable<void> => {
      return this.serveMicroservice(app);
    });
  }

  /**
   * Resolve the JetStream strategy, connect it to the host application, and
   * start all registered microservices.
   * @param app The running Nest application into which the strategy is mounted.
   * @returns Observable that completes once all microservices have started.
   */
  private serveMicroservice(app: NestFastifyApplication): Observable<void> {
    const strategy = app.get(JetstreamStrategy, { strict: false });

    app.connectMicroservice({ strategy }, { inheritAppConfig: true });

    return from(app.startAllMicroservices()).pipe(map(() => undefined));
  }
}
