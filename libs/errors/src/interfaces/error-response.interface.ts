export interface IErrorResponse {
  code: Uppercase<string>;
  details?: Record<string, unknown>;
  internal?: Record<string, unknown>;
  timestamp: string;
  requestId: string | null;
}
