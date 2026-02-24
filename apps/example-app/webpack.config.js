const path = require('path');
const { composePlugins, withNx } = require('@nx/webpack');
const WebpackBar = require('webpackbar');

module.exports = composePlugins(
  withNx({
    target: 'node',
    compiler: 'tsc',
    skipTypeChecking: false,
  }),
  (config) => {
    config.cache = { type: 'memory' };
    config.module = config.module || {};
    config.module.rules = config.module.rules || [];

    // 1. Cleanup default rules
    config.module.rules = config.module.rules.filter((rule) => {
      return !(rule && rule.test && rule.test.toString().includes('ts'));
    });

    // 2. Injection: Strict ts-loader
    config.module.rules.push({
      test: /\.tsx?$/,
      exclude: /node_modules/,
      use: [
        {
          loader: 'ts-loader',
          options: {
            configFile: path.resolve(__dirname, 'tsconfig.app.json'),
            // Typia requires type checking for transformation, so transpileOnly MUST be false
            // unless you use specific typia setup that allows it (rare).
            transpileOnly: false,
            experimentalWatchApi: true,
          },
        },
      ],
    });

    // 3. UI/UX: Add progress bar
    config.plugins.push(
      new WebpackBar({
        name: 'example-app',
        color: 'green',
        reporters: ['fancy'],
      }),
    );

    config.stats = {
      all: false,
      errors: true,
      warnings: true,
      timings: true,
      errorsCount: true,
      colors: true,
    };

    let isFirstRun = true;
    config.plugins.push({
      apply: (compiler) => {
        compiler.hooks.watchRun.tap('ClearScreen', () => {
          if (!isFirstRun) {
            process.stdout.write('\x1Bc');
          }
          isFirstRun = false;
        });

        compiler.hooks.done.tap('DoneMessage', (stats) => {
          if (stats.hasErrors()) {
            console.log('\n❌  \x1b[31mBuild failed with type errors!\x1b[0m\n');
          } else {
            console.log('\n✅  \x1b[32mBuild success! Waiting for changes...\x1b[0m\n');
          }
        });
      },
    });

    if (config.plugins) {
      config.plugins = config.plugins.filter((plugin) => {
        // Фільтруємо за іменем конструктора
        return plugin.constructor.name !== 'SourceMapDevToolPlugin';
      });
    }

    config.devtool = 'source-map';

    return config;
  },
);
