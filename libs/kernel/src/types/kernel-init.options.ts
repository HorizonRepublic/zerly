import { AppMode } from '../enum/app-mode.enum';

export interface IKernelInitOptions {
  /**
   * Application mode
   * @default 'server'
   */
  mode?: AppMode;
}
