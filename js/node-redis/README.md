# pubsub-sub-bench (node-redis Edition)

High-performance **Redis Pub/Sub benchmark tool**, written in Node.js.  
Supports both **standalone** and **Redis OSS Cluster** modes, with support for `PUBLISH`, `SPUBLISH`, `SUBSCRIBE`, and `SSUBSCRIBE`.

> Part of the [redis-performance/pubsub-sub-bench](https://github.com/redis-performance/pubsub-sub-bench) suite.

--- 

## 📦 Installation

```bash
npm install
```

## Usage

This version of the benchmark accepts the same arguments as the original go version
There's a script that can be utilizaed to start multiple instances of the benchmark

Usage: ./run-multi-bench.sh <instances> [benchmark_args...]

Example: ./run-multi-bench.sh 3 --mode=publish --clients=100 --test-time=60
