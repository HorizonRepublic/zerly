export interface IErrorContext {
  type: 'http' | 'rpc';
  requestId?: string;
  method?: string;
  url?: string;
}
