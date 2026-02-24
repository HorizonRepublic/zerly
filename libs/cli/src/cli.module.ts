import { Module } from '@nestjs/common';

import { GeneratorsModule } from './generators/generators.module';

@Module({
  imports: [GeneratorsModule],
})
export class CliModule {}
