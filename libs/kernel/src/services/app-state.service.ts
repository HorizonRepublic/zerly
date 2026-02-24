import { Inject, Injectable, Logger } from '@nestjs/common';

import {
  catchError,
  concatMap,
  defer,
  firstValueFrom,
  from,
  map,
  Observable,
  of,
  toArray,
} from 'rxjs';

import { AppState } from '@zerly/config';

import { APP_REF_SERVICE } from '../tokens';
import { IAppRefService, IAppStateService, IPrioritizedCallback, IStateCallback } from '../types';

/**
 * Default implementation of IAppStateService.
 *
 * Uses priority-sorted arrays to manage callbacks and RxJS streams
 * for sequential execution with error handling.
 *
 * @internal
 * @example - See IAppStateService
 */
@Injectable()
export class AppStateService implements IAppStateService {
  public state: AppState = AppState.NotReady;

  private readonly createdCbs: IPrioritizedCallback[] = [];
  private readonly listeningCbs: IPrioritizedCallback[] = [];
  private readonly logger = new Logger(AppStateService.name);

  public constructor(
    @Inject(APP_REF_SERVICE)
    private readonly appRef: IAppRefService,
  ) {}

  public onCreated(cb: IStateCallback, priority = 0): void {
    const prioritizedCb: IPrioritizedCallback = { callback: cb, priority };

    this.insertByPriority(this.createdCbs, prioritizedCb);

    // Execute immediately if state already passed
    if (this.state !== AppState.NotReady) {
      void firstValueFrom(this.runCb$(cb));
    }
  }

  public onListening(cb: IStateCallback, priority = 0): void {
    const prioritizedCb: IPrioritizedCallback = { callback: cb, priority };

    this.insertByPriority(this.listeningCbs, prioritizedCb);

    // Execute immediately if already listening
    if (this.state === AppState.Listening) {
      void firstValueFrom(this.runCb$(cb));
    }
  }

  public setState$(state: AppState): Observable<void> {
    this.state = state;
    this.logger.debug(`State changed to: ${state}`);

    const callbacks = this.getCallbacksForState(state);

    if (callbacks.length === 0) return of(void 0);

    // Execute all callbacks sequentially using concatMap
    return from(callbacks).pipe(
      concatMap(({ callback }) => this.runCb$(callback)),
      toArray(),
      map(() => void 0),
    );
  }

  private getCallbacksForState(state: AppState): IPrioritizedCallback[] {
    if (state === AppState.Created) return this.createdCbs;
    if (state === AppState.Listening) return this.listeningCbs;

    return [];
  }

  private insertByPriority(array: IPrioritizedCallback[], item: IPrioritizedCallback): void {
    const index = array.findIndex((cb) => cb.priority > item.priority);

    if (index === -1) {
      array.push(item);
    } else {
      array.splice(index, 0, item);
    }
  }

  private runCb$(cb: IStateCallback): Observable<void> {
    return defer(() => {
      const app = this.appRef.get();
      const result = cb(app);

      // Handle void return (undefined)
      if (!result) return of(void 0);

      return from(result);
    }).pipe(
      catchError((err) => {
        this.logger.error('Callback execution failed:', err);

        return of(void 0);
      }),
    );
  }
}
