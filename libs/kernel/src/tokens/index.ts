import { Brand, ServiceToken } from '@zerly/core';

export const APP_REF_SERVICE = Symbol(`app-reference-service`) as ServiceToken<Brand.App>;

export const APP_STATE_SERVICE = Symbol(`app-state-service`) as ServiceToken<Brand.App>;
