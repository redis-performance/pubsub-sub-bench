const Redis = require('ioredis');
const clusterKeySlot = require('cluster-key-slot');
const { publisherRoutine } = require('./publisher');
const { subscriberRoutine } = require('./subscriber');
const { updateCLI, writeFinalResults } = require('./metrics');
const seedrandom = require('seedrandom');

async function runBenchmark(argv) {
  console.log(`pubsub-sub-bench (JavaScript version)`);
  console.log(`Using random seed: ${argv['rand-seed']}`);
  Math.random = seedrandom(argv['rand-seed'].toString());

  if (argv['measure-rtt-latency']) {
    console.log('RTT measurement enabled.');
  }

  if (argv.verbose) {
    console.log('Verbose mode enabled.');
  }

  // Shared mutable state (as references)
  const totalMessagesRef = { value: 0 };
  const totalSubscribedRef = { value: 0 };
  const totalPublishersRef = { value: 0 };
  const totalConnectsRef = { value: 0 };
  const isRunningRef = { value: true };
  const messageRateTs = [];
  const rttValues = [];

  const redisOptions = {
    host: argv.host,
    port: argv.port,
    username: argv.user || undefined,
    password: argv.a || undefined,
    connectTimeout: argv['redis-timeout'],
    commandTimeout: argv['redis-timeout'],
    maxRetriesPerRequest: 1,
    enableReadyCheck: true,
    lazyConnect: false
  };

  if (argv['pool-size'] > 0) {
    redisOptions.connectionPoolSize = argv['pool-size'];
    redisOptions.maxConnections = argv['pool-size'];
  }

  let clients = [];
  let nodeAddresses = [];
  let slotClientMap = new Map();

  if (argv['oss-cluster-api-distribute-subscribers']) {
    const cluster = new Redis.Cluster(
      [
        {
          host: argv.host,
          port: argv.port
        }
      ],
      {
        redisOptions,
        scaleReads: 'master',
        enableReadyCheck: true,
        lazyConnect: false
      }
    );

    // Fetch slot info to build map
    const slots = await cluster.cluster('slots');
    if (!slots || slots.length === 0) {
      throw new Error('Cluster has no slot assignments. Check node health.');
    }

    // Create a standalone client for each slot range
    for (const [startSlot, endSlot, [ip, port]] of slots) {
      const client = new Redis({
        host: ip,
        port,
        username: argv.user || undefined,
        password: argv.a || undefined,
        connectTimeout: argv['redis-timeout'],
        commandTimeout: argv['redis-timeout'],
        maxRetriesPerRequest: 1,
        enableReadyCheck: true,
        lazyConnect: false
      });

      // Save one entry per slot
      for (let slot = startSlot; slot <= endSlot; slot++) {
        slotClientMap.set(slot, client);
      }

      nodeAddresses.push(`${ip}:${port}`);
      console.log(`Cluster mode - using ${nodeAddresses.length} unique nodes`);
    }
  } else {
    const client = new Redis(redisOptions);
    // Redis Cluster hash slots range: 0 - 16383
    for (let slot = 0; slot <= 16383; slot++) {
      slotClientMap.set(slot, client);
    }

    nodeAddresses.push(`${argv.host}:${argv.port}`);
    console.log('Standalone mode - using single Redis instance');
  }

  const totalChannels = argv['channel-maximum'] - argv['channel-minimum'] + 1;
  const totalSubscriptions = totalChannels * argv['subscribers-per-channel'];
  const totalExpectedMessages = totalSubscriptions * argv.messages;
  const subscriptionsPerNode = Math.ceil(totalSubscriptions / clients.length);

  if (argv['pool-size'] === 0) {
    redisOptions.connectionPoolSize = subscriptionsPerNode;
    redisOptions.maxConnections = subscriptionsPerNode;
    console.log(`Setting per Node connection pool size to ${subscriptionsPerNode}`);
  }

  console.log(`Will use a subscriber prefix of: ${argv['subscriber-prefix']}<channel id>`);
  console.log(`Total channels: ${totalChannels}`);
  console.log('Final setup used for benchmark:');
  nodeAddresses.forEach((addr, i) => {
    console.log(`Node #${i}: Address: ${addr}`);
  });

  const promises = [];

  if (argv.mode.includes('subscribe')) {
    if (argv['subscribers-placement-per-channel'] === 'dense') {
      for (let clientId = 1; clientId <= argv.clients; clientId++) {
        const channels = [];
        const numChannels = pickChannelCount(argv);

        for (let i = 0; i < numChannels; i++) {
          const id = randomChannel(argv);
          channels.push(`${argv['subscriber-prefix']}${id}`);
        }

        const subscriberName = `subscriber#${clientId}`;
        const slot = clusterKeySlot(channels[0]);
        const client = slotClientMap.get(slot);

        const reconnectInterval = randomInt(
          argv['min-reconnect-interval'],
          argv['max-reconnect-interval']
        );

        if (reconnectInterval > 0) {
          console.log(`Reconnect interval for ${subscriberName}: ${reconnectInterval}ms`);
        }

        console.log(`${subscriberName} subscribing to ${channels.length} channels.`);

        promises.push(
          subscriberRoutine(
            subscriberName,
            argv.mode,
            channels,
            argv['print-messages'],
            reconnectInterval,
            argv['measure-rtt-latency'],
            client,
            isRunningRef,
            rttValues,
            totalMessagesRef,
            totalSubscribedRef,
            totalConnectsRef,
            argv.verbose
          )
        );
      }
    }
  } else {
    console.error(`Invalid mode '${argv.mode}'. Use: subscribe, ssubscribe, publish, spublish`);
    process.exit(1);
  }

  const { startTime, now, perSecondStats } = await updateCLI(
    argv['client-update-tick'],
    argv.messages > 0 ? totalExpectedMessages : 0,
    argv['test-time'],
    argv['measure-rtt-latency'],
    argv.mode,
    isRunningRef,
    totalMessagesRef,
    totalConnectsRef,
    totalSubscribedRef,
    totalPublishersRef,
    messageRateTs,
    rttValues,
    () => {} // no-op, outputResults is handled after await
  );

  // Wait for all routines to finish
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
    rttValues,
    perSecondStats
  );

  // cleanly exit the process once done
  process.exit(0);
}

function randomInt(min, max) {
  if (min === max) return min;
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

function pickChannelCount(argv) {
  return randomInt(
    argv['min-number-channels-per-subscriber'],
    argv['max-number-channels-per-subscriber']
  );
}

function randomChannel(argv) {
  return (
    Math.floor(Math.random() * (argv['channel-maximum'] - argv['channel-minimum'] + 1)) +
    argv['channel-minimum']
  );
}

function pickClient(argv, clients, channel, clientId) {
  if (argv.mode.startsWith('s') && argv['oss-cluster-api-distribute-subscribers']) {
    const slot = clusterKeySlot(channel);
    return clients[slot % clients.length];
  } else {
    return clients[clientId % clients.length];
  }
}

module.exports = { runBenchmark };
