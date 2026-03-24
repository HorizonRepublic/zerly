import { tags } from 'typia';

export interface IDbConfig {
  host: string & tags.Default<'localhost'>;
  port: number & tags.Default<5432>;
  database: string & tags.Default<'public'>;
  username: string;
  password: string;
}
