import { INestApplication, Inject, Injectable, Logger } from '@nestjs/common';
import { CustomStrategy } from '@nestjs/microservices';

import { getJetStreamTransportToken } from '@horizon-republic/nestjs-jetstream';
import { DebugEvents, Events } from 'nats';
import { from, map, Observable } from 'rxjs';

import { APP_STATE_SERVICE, IAppStateService } from '@zerly/kernel';

import { MICROSERVICE_OPTIONS } from '../const';
import { IMicroserviceModuleOptions } from '../types/microservice-module.options';

@Injectable()
export class MicroserviceServerProvider {
  private readonly logger = new Logger(MicroserviceServerProvider.name);

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

  private serveMicroservice(app: INestApplication): Observable<void> {
    const transport = app.get<CustomStrategy>(getJetStreamTransportToken(this.options.name));

    const microservice = app.connectMicroservice<CustomStrategy>(transport, {
      inheritAppConfig: true,
    });

    microservice.on(Events.Reconnect, () => {
      this.logger.log('(client log) Reconnected to NATS');
    });

    microservice.on(DebugEvents.PingTimer, () => {
      this.logger.log('(client log) PING NATS');
    });

    return from(app.startAllMicroservices()).pipe(map(() => void 0));
  }
}
