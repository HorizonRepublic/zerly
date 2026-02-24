import { Brand, ServiceToken } from '@zerly/core';

export const ENV_METADATA_KEY = Symbol('env-metadata') as ServiceToken<Brand.Config>;

export const APP_CONFIG = Symbol('app-config') as ServiceToken<Brand.Config>;
