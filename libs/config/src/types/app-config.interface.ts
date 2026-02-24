import { LevelWithSilent } from 'pino';
import { tags } from 'typia';
import { Default } from 'typia/lib/tags/Default';
import { Pattern } from 'typia/lib/tags/Pattern';

import { Environment } from '../enums/kernel.enum';

export interface IAppConfig {
  readonly env: Environment;

  readonly host: string & tags.Default<'127.0.0.1'>;

  readonly name: string & Pattern<'^[a-z][a-z0-9]*(-[a-z0-9]+)*$'>;

  readonly port: number & tags.Type<'uint32'>;

  readonly generateEnvExample: boolean & Default<true>;

  readonly logLever: LevelWithSilent & Default<'info'>;
}
