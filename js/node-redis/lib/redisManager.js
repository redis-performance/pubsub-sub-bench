// filepath: /Users/hristo.temelski/code/etc/pubsub-sub-bench/js/node-redis/lib/redisManager.js
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

  // Configure Redis client options
  const redisOptions = {
    socket: {
      host: argv.host,
      port: argv.port,
      connectTimeout: argv['redis-timeout'],
      reconnectStrategy: false  // disable auto-reconnect
    },
    password: argv.a || undefined,
    username: argv.user || undefined
  };

  let clientsMap = new Map();
  let nodeAddresses = [];
  let isClusterMode = false;

  if (argv['oss-cluster-api-distribute-subscribers']) {
    isClusterMode = true;
    console.log('Using Redis Cluster mode');
    
    try {
      // Create a test cluster client to get nodes
      const testCluster = createCluster({
        rootNodes: [
          {
            socket: {
              host: argv.host,
              port: argv.port
            },
            password: argv.a || undefined,
            username: argv.user || undefined
          }
        ]
      });
      
      await testCluster.connect();
      
      // Get cluster slots information to determine node distribution
      const slots = await testCluster.clusterSlots();
      await testCluster.quit();
      
      if (!slots || Object.keys(slots).length === 0) {
        throw new Error('Cluster has no slot assignments. Check node health.');
      }
      
      // Create a map of slots to nodes
      for (const slotRange of Object.values(slots)) {
        const [startSlot, endSlot] = slotRange.slots;
        const { host, port } = slotRange.master;
        
        // Create a standalone client for each node
        const nodeId = `${host}:${port}`;
        if (!clientsMap.has(nodeId)) {
          const nodeOptions = {
            socket: {
              host,
              port,
              connectTimeout: argv['redis-timeout'],
              reconnectStrategy: false
            },
            password: argv.a || undefined,
            username: argv.user || undefined
          };
          
          nodeAddresses.push(nodeId);
          clientsMap.set(nodeId, nodeOptions);
        }
        
        // Map each slot to this node
        for (let slot = startSlot; slot <= endSlot; slot++) {
          clientsMap.set(`slot:${slot}`, nodeId);
        }
      }
      
      console.log(`Cluster mode - using ${nodeAddresses.length} unique nodes`);
    } catch (err) {
      console.error('Failed to initialize cluster mode:', err);
      process.exit(1);
    }
  } else {
    // Single node mode
    nodeAddresses.push(`${argv.host}:${argv.port}`);
    clientsMap.set(nodeAddresses[0], redisOptions);
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

  // Helper function to get a slot for a key (simplified version)
  function getSlot(key) {
    const crc16tab = [
      0x0000, 0x1021, 0x2042, 0x3063, 0x4084, 0x50a5, 0x60c6, 0x70e7,
      0x8108, 0x9129, 0xa14a, 0xb16b, 0xc18c, 0xd1ad, 0xe1ce, 0xf1ef,
      0x1231, 0x0210, 0x3273, 0x2252, 0x52b5, 0x4294, 0x72f7, 0x62d6,
      0x9339, 0x8318, 0xb37b, 0xa35a, 0xd3bd, 0xc39c, 0xf3ff, 0xe3de,
      0x2462, 0x3443, 0x0420, 0x1401, 0x64e6, 0x74c7, 0x44a4, 0x5485,
      0xa56a, 0xb54b, 0x8528, 0x9509, 0xe5ee, 0xf5cf, 0xc5ac, 0xd58d,
      0x3653, 0x2672, 0x1611, 0x0630, 0x76d7, 0x66f6, 0x5695, 0x46b4,
      0xb75b, 0xa77a, 0x9719, 0x8738, 0xf7df, 0xe7fe, 0xd79d, 0xc7bc,
      0x48c4, 0x58e5, 0x6886, 0x78a7, 0x0840, 0x1861, 0x2802, 0x3823,
      0xc9cc, 0xd9ed, 0xe98e, 0xf9af, 0x8948, 0x9969, 0xa90a, 0xb92b,
      0x5af5, 0x4ad4, 0x7ab7, 0x6a96, 0x1a71, 0x0a50, 0x3a33, 0x2a12,
      0xdbfd, 0xcbdc, 0xfbbf, 0xeb9e, 0x9b79, 0x8b58, 0xbb3b, 0xab1a,
      0x6ca6, 0x7c87, 0x4ce4, 0x5cc5, 0x2c22, 0x3c03, 0x0c60, 0x1c41,
      0xedae, 0xfd8f, 0xcdec, 0xddcd, 0xad2a, 0xbd0b, 0x8d68, 0x9d49,
      0x7e97, 0x6eb6, 0x5ed5, 0x4ef4, 0x3e13, 0x2e32, 0x1e51, 0x0e70,
      0xff9f, 0xefbe, 0xdfdd, 0xcffc, 0xbf1b, 0xaf3a, 0x9f59, 0x8f78,
      0x9188, 0x81a9, 0xb1ca, 0xa1eb, 0xd10c, 0xc12d, 0xf14e, 0xe16f,
      0x1080, 0x00a1, 0x30c2, 0x20e3, 0x5004, 0x4025, 0x7046, 0x6067,
      0x83b9, 0x9398, 0xa3fb, 0xb3da, 0xc33d, 0xd31c, 0xe37f, 0xf35e,
      0x02b1, 0x1290, 0x22f3, 0x32d2, 0x4235, 0x5214, 0x6277, 0x7256,
      0xb5ea, 0xa5cb, 0x95a8, 0x8589, 0xf56e, 0xe54f, 0xd52c, 0xc50d,
      0x34e2, 0x24c3, 0x14a0, 0x0481, 0x7466, 0x6447, 0x5424, 0x4405,
      0xa7db, 0xb7fa, 0x8799, 0x97b8, 0xe75f, 0xf77e, 0xc71d, 0xd73c,
      0x26d3, 0x36f2, 0x0691, 0x16b0, 0x6657, 0x7676, 0x4615, 0x5634,
      0xd94c, 0xc96d, 0xf90e, 0xe92f, 0x99c8, 0x89e9, 0xb98a, 0xa9ab,
      0x5844, 0x4865, 0x7806, 0x6827, 0x18c0, 0x08e1, 0x3882, 0x28a3,
      0xcb7d, 0xdb5c, 0xeb3f, 0xfb1e, 0x8bf9, 0x9bd8, 0xabbb, 0xbb9a,
      0x4a75, 0x5a54, 0x6a37, 0x7a16, 0x0af1, 0x1ad0, 0x2ab3, 0x3a92,
      0xfd2e, 0xed0f, 0xdd6c, 0xcd4d, 0xbdaa, 0xad8b, 0x9de8, 0x8dc9,
      0x7c26, 0x6c07, 0x5c64, 0x4c45, 0x3ca2, 0x2c83, 0x1ce0, 0x0cc1,
      0xef1f, 0xff3e, 0xcf5d, 0xdf7c, 0xaf9b, 0xbfba, 0x8fd9, 0x9ff8,
      0x6e17, 0x7e36, 0x4e55, 0x5e74, 0x2e93, 0x3eb2, 0x0ed1, 0x1ef0
    ];
    
    let crc = 0;
    for (let i = 0; i < key.length; i++) {
      crc = ((crc << 8) ^ crc16tab[((crc >> 8) ^ key.charCodeAt(i)) & 0xff]) & 0xffff;
    }
    return crc % 16384;  // 16384 slots in Redis Cluster
  }

  // Helper to get Redis options for a specific channel
  function getRedisOptionsForChannel(channel) {
    if (!isClusterMode) {
      return redisOptions;
    }
    
    const slot = getSlot(channel);
    const nodeId = clientsMap.get(`slot:${slot}`);
    return clientsMap.get(nodeId);
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
      const channelOptions = getRedisOptionsForChannel(channels[0]);

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
          channelOptions,
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
        const channelOptions = getRedisOptionsForChannel(channels[0]);

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
            channelOptions,
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

  // Clean exit
  process.exit(0);
}

module.exports = { runBenchmark };