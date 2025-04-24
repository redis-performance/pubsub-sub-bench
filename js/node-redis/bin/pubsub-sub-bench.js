#!/usr/bin/env node
// filepath: /Users/hristo.temelski/code/etc/pubsub-sub-bench/js/node-redis/bin/pubsub-sub-bench.js

const { parseArgs } = require('../lib/config');
const { runBenchmark } = require('../lib/redisManager');

// Parse command line arguments
const argv = parseArgs();

// Run the benchmark
runBenchmark(argv).catch(err => {
  console.error('Benchmark execution error:', err);
  process.exit(1);
});