import { ConfigFormat } from '@zerly/config';

import { AppMode } from '../enum/app-mode.enum';

export interface IKernelInitOptions {
  /**
   * Application mode.
   * @default AppMode.Server
   */
  mode?: AppMode;

  /**
   * Configuration source format.
   * @default ConfigFormat.Dotenv
   */
  envFormat?: ConfigFormat;
}
