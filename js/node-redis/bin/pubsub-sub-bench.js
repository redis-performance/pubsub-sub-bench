#!/usr/bin/env node

const { parseArgs } = require('../lib/config');
const { runBenchmark } = require('../lib/redisManager');

(async () => {
  const argv = parseArgs();

  try {
    await runBenchmark(argv);
  } catch (err) {
    console.error('Error in main execution:', err);
    process.exit(1);
  }
})();
