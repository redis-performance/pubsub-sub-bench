const Redis = require('ioredis');
const clusterKeySlot = require('cluster-key-slot');
const { publisherRoutine } = require('./publisher');
const { subscriberRoutine } = require('./subscriber');
const { updateCLI, writeFinalResults, createRttHistogram, RttAccumulator } = require('./metrics');
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
  
  // Create efficient RTT tracking
  const rttAccumulator = argv['measure-rtt-latency'] ? new RttAccumulator() : null;
  // Create histogram for RTT recording
  const rttHistogram = argv['measure-rtt-latency'] ? createRttHistogram() : null;

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
  let cluster = null;
  console.log(`Using ${argv['slot-refresh-interval']} slot-refresh-interval`);
  console.log(`Using ${argv['redis-timeout']} redis-timeout`);

  if (argv['oss-cluster-api-distribute-subscribers']) {
    // Create N independent cluster clients for publishers
    const numClusterClients = argv.clients;

    console.log(`\nCluster mode - creating ${numClusterClients} cluster clients...`);

    const clusterOptions = {
      redisOptions,
      scaleReads: 'master',
      enableReadyCheck: true,
      lazyConnect: false,
      connectTimeout: argv['redis-timeout'],
      slotsRefreshInterval: argv['slot-refresh-interval'],
      enableOfflineQueue: true,
      retryDelayOnClusterDown: 300,
      retryDelayOnFailover: 100,
      maxRedirections: 16,
      maxRetriesPerRequest: null
    };

    // Create N cluster clients
    for (let i = 0; i < numClusterClients; i++) {
      const clusterClient = new Redis.Cluster(
        [{ host: argv.host, port: argv.port }],
        clusterOptions
      );

      clusterClient.setMaxListeners(0); // Unlimited listeners
      clients.push(clusterClient);

      // Wait for cluster client to be ready
      await new Promise((resolve, reject) => {
        clusterClient.on('ready', resolve);
        clusterClient.on('error', reject);
      });

      // Use the first cluster client to discover topology
      if (i === 0) {
        cluster = clusterClient;
        const slotsMapping = await clusterClient.cluster('SLOTS');

        console.log(`Cluster mode - discovered ${slotsMapping.length} master nodes\n`);
        console.log(`Cluster SLOTS mapping:`);

        for (const slotRange of slotsMapping) {
          const startSlot = slotRange[0];
          const endSlot = slotRange[1];
          const host = slotRange[2][0];
          const port = slotRange[2][1];
          const nodeAddr = `${host}:${port}`;

          console.log(`  Slots ${startSlot}-${endSlot}: ${nodeAddr}`);

          if (!nodeAddresses.includes(nodeAddr)) {
            nodeAddresses.push(nodeAddr);
          }
        }
        console.log('');
      }

      if ((i + 1) % 10 === 0 || i === numClusterClients - 1) {
        console.log(`  Created ${i + 1}/${numClusterClients} cluster clients...`);
      }
    }

    console.log(`\nCluster mode - created ${clients.length} cluster clients`);
  } else {
    const client = new Redis(redisOptions);
    clients.push(client);
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
  const subscriptionsPerNode = Math.ceil(totalSubscriptions / nodeAddresses.length);

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


  if (argv.mode.includes('publish')) {
    // Run publishers
    totalPublishersRef.value = argv.clients;
    console.log(`Starting ${argv.clients} publishers in ${argv.mode} mode`);
    
    for (let clientId = 1; clientId <= argv.clients; clientId++) {
      const channels = [];
      const numChannels = pickChannelCount(argv);

      for (let i = 0; i < numChannels; i++) {
        const channelId = randomChannel(argv);
        const channelName = `${argv['subscriber-prefix']}${channelId}`;
        channels.push(channelName);
      }

      const publisherName = `publisher#${clientId}`;

      // Round-robin through the cluster clients
      const client = clients[(clientId - 1) % clients.length];

      if (argv.verbose) {
        const slot = argv.mode.startsWith('s') && argv['oss-cluster-api-distribute-subscribers']
          ? clusterKeySlot(channels[0])
          : 'N/A';
        console.log(`Publisher ${clientId}: channel=${channels[0]}, slot=${slot}, client=${(clientId - 1) % clients.length}/${clients.length}`);
      }

      const skipDuplicate = true; // Don't duplicate cluster clients

      promises.push(
        publisherRoutine(
          publisherName,
          channels,
          argv.mode,
          argv['measure-rtt-latency'],
          argv.verbose,
          argv['data-size'],
          client,
          isRunningRef,
          totalMessagesRef,
          null, // rateLimiter
          skipDuplicate
        )
      );
      
      totalConnectsRef.value++;
      
      if (clientId % 100 === 0) {
        console.log(`Created ${clientId} publishers so far.`);
      }
    }
  } else if (argv.mode.includes('subscribe')) {
    // Only run subscribers
    if (argv['subscribers-placement-per-channel'] === 'dense') {
      for (let clientId = 1; clientId <= argv.clients; clientId++) {
        const channels = [];
        const numChannels = pickChannelCount(argv);

        for (let i = 0; i < numChannels; i++) {
          const id = randomChannel(argv);
          channels.push(`${argv['subscriber-prefix']}${id}`);
        }

        const subscriberName = `subscriber#${clientId}`;
        let client;

        // For sharded pub/sub in cluster mode, we need standalone clients for subscribers
        // because ioredis Cluster clients don't support SSUBSCRIBE properly
        if (argv.mode.startsWith('s') && argv['oss-cluster-api-distribute-subscribers']) {
          const slot = clusterKeySlot(channels[0]);

          // Get node info from the first cluster client
          const nodeKeys = cluster.slots[slot];
          if (!nodeKeys || nodeKeys.length === 0) {
            console.error(`No node found for slot ${slot} (channel: ${channels[0]})`);
            process.exit(1);
          }

          const masterKey = nodeKeys[0];
          const [host, port] = masterKey.split(':');

          // Create a standalone Redis client connected directly to the node
          client = new Redis({
            host,
            port: parseInt(port),
            username: argv.user || undefined,
            password: argv.a || undefined,
            connectTimeout: argv['redis-timeout'],
            commandTimeout: argv['redis-timeout'],
            maxRetriesPerRequest: 1,
            enableReadyCheck: true,
            lazyConnect: false
          });

          client.setMaxListeners(0);

          // Wait for client to be ready
          await new Promise((resolve, reject) => {
            if (client.status === 'ready') {
              resolve();
            } else {
              client.once('ready', resolve);
              client.once('error', reject);
            }
          });

          if (argv.verbose) {
            console.log(`Subscriber ${clientId}: channel=${channels[0]}, slot=${slot}, node=${host}:${port}`);
          }
        } else {
          // Standalone mode - use first client
          client = clients[0];
        }

        const reconnectInterval = randomInt(
          argv['min-reconnect-interval'],
          argv['max-reconnect-interval']
        );

        if (reconnectInterval > 0) {
          console.log(`Reconnect interval for ${subscriberName}: ${reconnectInterval}ms`);
        }

        const skipDuplicate = true; // Don't duplicate - we already created a dedicated client

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
            rttAccumulator,
            rttHistogram,
            totalMessagesRef,
            totalSubscribedRef,
            totalConnectsRef,
            argv.verbose,
            argv.clients,
            skipDuplicate
          )
        );
      }
    }
  } else {
    console.error(`Invalid mode '${argv.mode}'. Use: subscribe, ssubscribe, publish, spublish`);
    process.exit(1);
  }

  try {
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
      rttAccumulator,
      rttHistogram,
      () => {} // no-op, outputResults is handled after await
    );

    // Wait for all routines to finish
    console.log('Waiting for all clients to shut down cleanly...');
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
    // Clean shutdown of primary clients
    console.log('Shutting down primary Redis connections...');
    
    // Close cluster client if it exists
    if (cluster) {
      try {
        await cluster.quit();
        console.log('Cluster client disconnected successfully');
      } catch (err) {
        console.error('Error disconnecting cluster client:', err);
      }
    }
    
    // Close all standalone clients
    const disconnectPromises = clients.map(async (client, i) => {
      try {
        await client.quit();
        if (argv.verbose) {
          console.log(`Node client #${i} disconnected successfully`);
        }
      } catch (err) {
        console.error(`Error disconnecting node client #${i}:`, err);
      }
    });
    
    await Promise.all(disconnectPromises);
    console.log('All Redis connections closed');
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
