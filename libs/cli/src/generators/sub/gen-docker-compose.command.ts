import { existsSync } from 'node:fs';
import { writeFile } from 'node:fs/promises';
import { join, relative } from 'node:path';

import { CommandRunner, Option, SubCommand } from 'nest-commander';

import { DOCKER_COMPOSE_TEMPLATE } from './const';

interface IDockerComposeOptions {
  force?: boolean;
  dryRun?: boolean;
}

@SubCommand({
  name: 'infra:docker',
  aliases: ['docker', 'dc'],
  description: 'Generates a docker-compose.yaml file for the project infrastructure.',
})
export class GenDockerComposeCommand extends CommandRunner {
  private readonly fileName = 'docker-compose.yaml';

  public async run(inputs: string[], options: IDockerComposeOptions): Promise<void> {
    const targetPath = join(process.cwd(), this.fileName);
    const relativePath = relative(process.cwd(), targetPath);

    console.log(`🐳  Preparing to generate infrastructure configuration...`);

    if (options.dryRun) {
      console.log(`\n[DRY-RUN] File would be created at: ${targetPath}`);
      console.log(`[DRY-RUN] Content preview:\n`);
      console.log(DOCKER_COMPOSE_TEMPLATE);
      return;
    }

    if (existsSync(targetPath) && !options.force) {
      console.error(
        `\n❌ Error: "${relativePath}" already exists.\n` +
          `   Use --force flag to overwrite it.\n` +
          `   Example: zerly g docker --force`,
      );
      process.exit(1);
    }

    try {
      await writeFile(targetPath, DOCKER_COMPOSE_TEMPLATE, 'utf8');

      console.log(`\n✅ Successfully generated: ${relativePath}`);
      console.log(`\n👉 Next steps:`);
      console.log(`   1. Review the configuration.`);
      console.log(`   2. Run infrastructure: docker compose up -d`);
    } catch (error) {
      console.error(`\n❌ Failed to write file: ${(error as Error).message}`);
      process.exit(1);
    }
  }

  @Option({
    flags: '-f, --force',
    description: 'Overwrite existing file if it exists',
    defaultValue: false,
  })
  public parseForce(): boolean {
    return true;
  }

  @Option({
    flags: '-d, --dry-run',
    description: 'Output the file content to console without creating it',
    defaultValue: false,
  })
  public parseDryRun(): boolean {
    return true;
  }
}
