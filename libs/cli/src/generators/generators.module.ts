import { Module } from '@nestjs/common';

import { GenerateCommand } from './generate.command';
import { GenDockerComposeCommand } from './sub/gen-docker-compose.command';

@Module({
  providers: [GenerateCommand, GenDockerComposeCommand],
})
export class GeneratorsModule {}
