import { existsSync } from 'node:fs';
import { dirname, join, normalize, resolve, sep } from 'node:path';

/** Known build output directories that indicate a compiled entry point. */
const BUILD_DIRS = [`${sep}dist${sep}`, `${sep}build${sep}`, `${sep}.next${sep}`];

/** Project root markers — at least one must exist. */
const PROJECT_MARKERS = ['package.json', 'project.json', 'tsconfig.json'];

/**
 * Returns `true` if `dirPath` exists and contains at least one project marker file.
 */
const isProjectDir = (dirPath: string): boolean =>
  existsSync(dirPath) && PROJECT_MARKERS.some((marker) => existsSync(join(dirPath, marker)));

/**
 * Resolves the application root directory by inspecting `process.argv[1]`.
 *
 * In monorepo setups (Nx, pnpm workspaces), the entry script often runs from
 * a build output directory (e.g. `dist/apps/my-app/main.js`). This function
 * strips known output directories to locate the source project root.
 *
 * Falls back to `process.cwd()` if no heuristic matches.
 */
export const resolveAppRoot = (): string => {
  const entryPath = process.argv[1];

  if (!entryPath) return process.cwd();

  const normalizedPath = normalize(entryPath);

  for (const buildDir of BUILD_DIRS) {
    if (normalizedPath.includes(buildDir)) {
      const potentialSourcePath = normalizedPath.replace(buildDir, sep);
      const potentialRoot = resolve(dirname(potentialSourcePath));
      const appRoot = resolve(potentialRoot, '..');

      if (isProjectDir(appRoot)) return appRoot;
      if (isProjectDir(potentialRoot)) return potentialRoot;
    }
  }

  return process.cwd();
};
