import { Default } from 'typia/lib/tags';

export interface IDbConfig {
  host: string & Default<'localhost'>;
  port: number & Default<5432>;
  database: string & Default<'public'>;
  username: string;
  password: string;
}
