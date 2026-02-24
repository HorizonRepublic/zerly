#!/usr/bin/env node
import { AppMode, Kernel } from '@zerly/kernel';

import { CliModule } from './cli.module';

Kernel.init(CliModule, { mode: AppMode.Cli }).subscribe({
  error: (err) => {
    console.error('Fatal CLI Error:', err);
    process.exit(1);
  },
});
