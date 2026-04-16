// @ts-nocheck — bench stack has its own tsconfig + npm install happens inside docker
import { Module } from '@nestjs/common';

import { BenchController } from './bench.controller';

/**
 * Minimal bench application module. No providers, no imports, no
 * global filters / pipes — only the BenchController. The whole
 * point of this stack is to measure the framework + adapter cost
 * with the smallest possible DI graph, so any extra provider or
 * module would pollute the numbers.
 */
@Module({
  controllers: [BenchController],
})
export class AppModule {}
