import { LevelWithSilent } from 'pino';
import { tags } from 'typia';

import { Environment } from '../enums/kernel.enum';

export interface IAppConfig {
  readonly env: Environment;

  readonly host: string & tags.Default<'127.0.0.1'>;

  readonly name: string & tags.Pattern<'^[a-z][a-z0-9]*(-[a-z0-9]+)*$'>;

  readonly port: number & tags.Type<'uint32'>;

  readonly generateEnvExample: boolean & tags.Default<true>;

  readonly logLevel: LevelWithSilent & tags.Default<'info'>;
}
