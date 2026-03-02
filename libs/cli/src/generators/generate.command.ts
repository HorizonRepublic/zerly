import { Command, CommandRunner } from 'nest-commander';

import { GenDockerComposeCommand } from './sub/gen-docker-compose.command';

@Command({
  name: 'generate',
  aliases: ['g'],
  description: 'Generates application resources',
  subCommands: [
    GenDockerComposeCommand,
    // other subcommands
  ],
})
export class GenerateCommand extends CommandRunner {
  public run(): Promise<void> {
    console.log('Please specify a resource to generate. Example: zerly g docker');

    return Promise.resolve();
  }
}
