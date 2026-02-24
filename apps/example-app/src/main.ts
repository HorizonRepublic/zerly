import { AppMode, Kernel } from '@zerly/kernel';

import { AppModule } from './app/app.module';

Kernel.init(AppModule, { mode: AppMode.Server });
