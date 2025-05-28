const yargs = require("yargs");

function parseArgs() {
  return yargs
    .option("host", { description: "Redis host", default: "127.0.0.1" })
    .option("port", { description: "Redis port", default: "6379" })
    .option("a", { description: "Password for Redis Auth", default: "" })
    .option("user", { description: "ACL-style AUTH username", default: "" })
    .option("data-size", { description: "Payload size in bytes", default: 128 })
    .option("mode", {
      description: "Mode: subscribe | ssubscribe | publish | spublish",
      default: "subscribe",
    })
    .option("subscribers-placement-per-channel", {
      description: "dense | sparse",
      default: "dense",
    })
    .option("channel-minimum", { description: "Min channel ID", default: 1 })
    .option("channel-maximum", { description: "Max channel ID", default: 100 })
    .option("subscribers-per-channel", {
      description: "Subscribers per channel",
      default: 1,
    })
    .option("clients", { description: "Number of connections", default: 50 })
    .option("min-number-channels-per-subscriber", { default: 1 })
    .option("max-number-channels-per-subscriber", { default: 1 })
    .option("min-reconnect-interval", { default: 0 })
    .option("max-reconnect-interval", { default: 0 })
    .option("messages", { default: 0 })
    .option("json-out-file", { default: "" })
    .option("client-update-tick", { default: 1 })
    .option("test-time", { default: 0 })
    .option("rand-seed", { default: 12345 })
    .option("subscriber-prefix", { default: "channel-" })
    .option("oss-cluster-api-distribute-subscribers", { default: false })
    .option("slot-refresh-interval", { default: -1 })
    .option("print-messages", { default: false })
    .option("verbose", { default: false })
    .option("measure-rtt-latency", { default: true })
    .option("redis-timeout", { default: 120000 })
    .option("pool-size", { default: 0 })
    .help().argv;
}

module.exports = { parseArgs };
