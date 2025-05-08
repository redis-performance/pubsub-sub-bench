const { createClient, createCluster } = require("redis");
const { publisherRoutine } = require("./publisher");
const { subscriberRoutine } = require("./subscriber");
const {
  updateCLI,
  writeFinalResults,
  createRttHistogram,
  RttAccumulator,
} = require("./metrics");
const { setTimeout } = require("node:timers/promises");
const seedrandom = require("seedrandom");

async function runBenchmark(argv) {
  console.log(`pubsub-sub-bench (node-redis version)`);
  console.log(`Using random seed: ${argv["rand-seed"]}`);
  Math.random = seedrandom(argv["rand-seed"].toString());

  if (argv["measure-rtt-latency"]) {
    console.log("RTT measurement enabled.");
  }

  if (argv.verbose) {
    console.log("Verbose mode enabled.");
  }

  // Shared mutable state (as references)
  const totalMessagesRef = { value: 0 };
  const totalSubscribedRef = { value: 0 };
  const totalPublishersRef = { value: 0 };
  const totalConnectsRef = { value: 0 };
  const isRunningRef = { value: true };
  const messageRateTs = [];

  // Create efficient RTT tracking
  const rttAccumulator = argv["measure-rtt-latency"]
    ? new RttAccumulator()
    : null;
  // Create histogram for RTT recording
  const rttHistogram = argv["measure-rtt-latency"]
    ? createRttHistogram()
    : null;

  const clientOptions = {
    socket: {
      host: argv.host,
      port: argv.port,
      connectTimeout: 120000,
    },
    username: argv.user || undefined,
    password: argv.a || undefined,
    disableClientInfo: true,
  };

  const clusterOptions = {
    rootNodes: [
      {
        disableClientInfo: true,
        socket: {
          host: argv.host,
          port: argv.port,
          connectTimeout: 120000,
          keepAlive: true,
        },
      },
    ],
    useReplicas: false,
    defaults: {
      disableClientInfo: true,
      username: argv.user || undefined,
      password: argv.a || undefined,
      connectTimeout: 120000,
      socket: {
        connectTimeout: 120000,
        keepAlive: true,
      },
    },
    minimizeConnections: true,
    connectTimeout: 120000,
  };

  console.log(`Using ${argv["slot-refresh-interval"]} slot-refresh-interval`);
  console.log(`Using ${argv["redis-timeout"]} redis-timeout`);

  let clients = [];
  let connectionPromises = [];

  for (let i = 1; i <= argv.clients; i++) {
    let client;
    if (argv["oss-cluster-api-distribute-subscribers"] === "true") {
      client = createCluster(clusterOptions);
    } else {
      client = createClient(clientOptions);
    }

    clients.push(client);
  }

  const totalChannels = argv["channel-maximum"] - argv["channel-minimum"] + 1;
  const totalSubscriptions = totalChannels * argv["subscribers-per-channel"];
  const totalExpectedMessages = totalSubscriptions * argv.messages;

  console.log(
    `Will use a subscriber prefix of: ${argv["subscriber-prefix"]}<channel id>`
  );
  console.log(`Total channels: ${totalChannels}`);
  console.log("Final setup used for benchmark:");

  const promises = [];

  if (argv.mode.includes("publish")) {
    // Run publishers
    totalPublishersRef.value = argv.clients;
    console.log(`Starting ${argv.clients} publishers in ${argv.mode} mode`);

    for (let clientId = 1; clientId <= argv.clients; clientId++) {
      const channels = [];
      const numChannels = pickChannelCount(argv);

      for (let i = 0; i < numChannels; i++) {
        const channelId = randomChannel(argv);
        const channelName = `${argv["subscriber-prefix"]}${channelId}`;
        channels.push(channelName);
      }

      const publisherName = `publisher#${clientId}`;

      if (argv.verbose) {
        console.log(`Publisher ${clientId} targeting channels ${channels}`);
      }

      promises.push(
        publisherRoutine(
          publisherName,
          channels,
          argv.mode,
          argv["measure-rtt-latency"],
          argv.verbose,
          argv["data-size"],
          clients[clientId - 1],
          isRunningRef,
          totalMessagesRef
        )
      );

      totalConnectsRef.value++;
    }
  } else if (argv.mode.includes("subscribe")) {
    // Only run subscribers
    if (argv["subscribers-placement-per-channel"] === "dense") {
      for (let clientId = 1; clientId <= argv.clients; clientId++) {
        const channels = [];
        const numChannels = pickChannelCount(argv);

        for (let i = 0; i < numChannels; i++) {
          const id = randomChannel(argv);
          channels.push(`${argv["subscriber-prefix"]}${id}`);
        }

        const subscriberName = `subscriber#${clientId}`;
        const reconnectInterval = randomInt(
          argv["min-reconnect-interval"],
          argv["max-reconnect-interval"]
        );

        if (reconnectInterval > 0) {
          console.log(
            `Reconnect interval for ${subscriberName}: ${reconnectInterval}ms`
          );
        }

        if (clientId % 100 === 0 || clientId === argv.clients) {
          console.log(
            `${subscriberName} subscribing to ${channels.length} channels.`
          );
        }

        promises.push(
          subscriberRoutine(
            subscriberName,
            argv.mode,
            channels,
            argv["print-messages"],
            reconnectInterval,
            argv["measure-rtt-latency"],
            clients[clientId - 1],
            isRunningRef,
            rttAccumulator,
            rttHistogram,
            totalMessagesRef,
            totalSubscribedRef,
            totalConnectsRef,
            argv.verbose,
            argv.clients
          )
        );
      }
    }
  } else {
    console.error(
      `Invalid mode '${argv.mode}'. Use: subscribe, ssubscribe, publish, spublish`
    );
    process.exit(1);
  }

  try {
    const { startTime, now, perSecondStats } = await updateCLI(
      argv["client-update-tick"],
      argv.messages > 0 ? totalExpectedMessages : 0,
      argv["test-time"],
      argv["measure-rtt-latency"],
      argv.mode,
      isRunningRef,
      totalMessagesRef,
      totalConnectsRef,
      totalSubscribedRef,
      totalPublishersRef,
      messageRateTs,
      rttAccumulator,
      rttHistogram
    );

    // Wait for all routines to finish
    console.log("Waiting for all clients to shut down cleanly...");
    await Promise.all(promises);

    // THEN output final results
    writeFinalResults(
      startTime,
      now,
      argv,
      argv.mode,
      totalMessagesRef.value,
      totalSubscribedRef.value,
      messageRateTs,
      rttAccumulator,
      rttHistogram,
      perSecondStats
    );
  } finally {
    // Clean shutdown of Redis connection
    console.log("Shutting down Redis connection...");
    try {
      for (let i = 0; i < argv.client; i++) {
        await clients[i].quit();
      }
      console.log("Redis connection closed successfully");
    } catch (err) {
      console.error("Error disconnecting Redis client:", err);
    }
  }

  // cleanly exit the process once done
  process.exit(0);
}

function randomInt(min, max) {
  if (min === max) return min;
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

function pickChannelCount(argv) {
  return randomInt(
    argv["min-number-channels-per-subscriber"],
    argv["max-number-channels-per-subscriber"]
  );
}

function randomChannel(argv) {
  return (
    Math.floor(
      Math.random() * (argv["channel-maximum"] - argv["channel-minimum"] + 1)
    ) + argv["channel-minimum"]
  );
}

module.exports = { runBenchmark };
