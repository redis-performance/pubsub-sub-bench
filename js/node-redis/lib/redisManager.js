const { createClient, createCluster } = require('redis');
const { publisherRoutine } = require('./publisher');
const { subscriberRoutine } = require('./subscriber');
const { updateCLI, writeFinalResults, createRttHistogram, RttAccumulator } = require('./metrics');
const seedrandom = require('seedrandom');

async function runBenchmark(argv) {
  console.log(`pubsub-sub-bench (node-redis version)`);
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

  let client;
  let nodeAddresses = [];

  if (argv['oss-cluster-api-distribute-subscribers']) {
    console.log('Using Redis Cluster mode');
    client = createCluster({
      rootNodes: [{
        socket: {
          host: argv.host,
          port: argv.port
        },
        password: argv.a || undefined,
        username: argv.user || undefined
      }],
      defaults: {
        socket: {
          connectTimeout: argv['redis-timeout'],
          reconnectStrategy: false // disable auto-reconnect
        }
      }
    });
    
    await client.connect();
    nodeAddresses = [`${argv.host}:${argv.port}`];
    console.log('Cluster mode - connecting through cluster client');
  } else {
    // Single node mode
    client = createClient({
      socket: {
        host: argv.host,
        port: argv.port,
        connectTimeout: argv['redis-timeout'],
        reconnectStrategy: false // disable auto-reconnect
      },
      password: argv.a || undefined,
      username: argv.user || undefined
    });
    
    await client.connect();
    nodeAddresses = [`${argv.host}:${argv.port}`];
    console.log('Standalone mode - using single Redis instance');
  }

  const totalChannels = argv['channel-maximum'] - argv['channel-minimum'] + 1;
  const totalSubscriptions = totalChannels * argv['subscribers-per-channel'];
  const totalExpectedMessages = totalSubscriptions * argv.messages;

  console.log(`Will use a subscriber prefix of: ${argv['subscriber-prefix']}<channel id>`);
  console.log(`Total channels: ${totalChannels}`);
  console.log('Final setup used for benchmark:');
  nodeAddresses.forEach((addr, i) => {
    console.log(`Node #${i}: Address: ${addr}`);
  });

  const promises = [];

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

      if (argv.verbose) {
        console.log(`Publisher ${clientId} targeting channels ${channels}`);
      }

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
          totalMessagesRef
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

        const reconnectInterval = randomInt(
          argv['min-reconnect-interval'],
          argv['max-reconnect-interval']
        );

        if (reconnectInterval > 0) {
          console.log(`Reconnect interval for ${subscriberName}: ${reconnectInterval}ms`);
        }

        if (clientId % 100 === 0 || clientId === argv.clients) {
          console.log(`${subscriberName} subscribing to ${channels.length} channels.`);
        }

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
            argv.clients
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

    // Output final results
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
  } catch (err) {
    console.error('Benchmark error:', err);
  }

  // Clean up and disconnect the main client
  try {
    await client.quit();
  } catch (err) {
    console.error('Error disconnecting main client:', err);
  }

  // Clean exit
  process.exit(0);
}

module.exports = { runBenchmark };