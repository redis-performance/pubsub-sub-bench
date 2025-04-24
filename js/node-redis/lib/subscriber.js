// filepath: /Users/hristo.temelski/code/etc/pubsub-sub-bench/js/node-redis/lib/subscriber.js
const { createClient } = require('redis');

async function subscriberRoutine(
  clientName,
  mode,
  channels,
  printMessages,
  reconnectInterval,
  measureRTT,
  redisOptions,
  isRunningRef,
  rttAccumulator,
  rttHistogram,
  totalMessagesRef,
  totalSubscribedRef,
  totalConnectsRef,
  verbose,
  totalClients
) {
  let client = null;
  let reconnectTimer = null;

  // Subscribe function
  const subscribe = async () => {
    try {
      // If already subscribed, disconnect and create new client
      if (client) {
        await client.quit();
      }

      // Create a new Redis client
      client = createClient(redisOptions);

      // Set up error handling
      client.on('error', (err) => {
        console.error(`[${clientName}] Redis error: ${err.message}`);
      });

      // Connect to Redis
      await client.connect();
      
      // Subscribe to channels with appropriate method
      if (mode === 'ssubscribe') {
          await client.sSubscribe(...channels, handleMessage);
      } else {
          await client.subscribe(...channels, handleMessage);
      }

      totalSubscribedRef.value += channels.length;
      totalConnectsRef.value++;
      
      if (verbose) {
        console.log(`[${clientName}] Successfully subscribed to ${channels.length} channels`);
      }
    } catch (err) {
      console.error(`[${clientName}] Subscribe error:`, err);
    }
  };

  // Handler for incoming messages
  const handleMessage = (message, channel) => {
    if (printMessages) {
      console.log(`[${clientName}] ${channel}: ${message}`);
    }

    if (measureRTT) {
      try {
        const now = Date.now();
        const timestamp = Number(message); // Timestamp from publisher
        const rtt = now - timestamp;
        if (rtt >= 0) {
          // Add to accumulator for per-tick average calculation
          if (rttAccumulator) {
            rttAccumulator.add(rtt);
          }
          // Record directly to histogram for final stats
          if (rttHistogram) {
            rttHistogram.recordValue(rtt);
          }
          if (verbose) {
            console.log(`[${clientName}] RTT: ${rtt} ms`);
          }
        } else {
          console.warn(`[${clientName}] Skipping negative RTT: now=${now}, ts=${timestamp}`);
        }
      } catch (err) {
        console.error(`[${clientName}] Invalid RTT message: ${message}`, err);
      }
    }
    totalMessagesRef.value++;
  };

  // Initial subscription
  await subscribe();

  // Set up automatic re-subscription if reconnectInterval is set
  if (reconnectInterval > 0) {
    reconnectTimer = setInterval(async () => {
      if (isRunningRef.value) {
        await subscribe();
      }
    }, reconnectInterval);
  }

  // Shutdown function
  const shutdown = async () => {
    // Clear the reconnection timer if set
    if (reconnectTimer) clearInterval(reconnectTimer);

    // Attempt to unsubscribe and disconnect
    try {
      if (client) {
        if (client.isOpen) {
          if (mode === 'ssubscribe') {
            for (const channel of channels) {
              await client.sUnsubscribe(channel);
            }
          } else {
            for (const channel of channels) {
              await client.unsubscribe(channel);
            }
          }
          await client.quit();
        }
      }
    } catch (err) {
      console.warn(`[${clientName}] Shutdown error: ${err.message}`);
    }
  };

  // Return a promise that waits until isRunningRef becomes false, then cleans up
  return new Promise((resolve) => {
    const check = setInterval(async () => {
      if (!isRunningRef.value) {
        clearInterval(check);
        const clientId = parseInt(clientName.split('#')[1], 10);
        const shouldLog = clientId % 100 === 0 || clientId === totalClients;

        if (shouldLog) console.log(`[${clientName}] Triggering shutdown...`);

        await shutdown();
        if (shouldLog) console.log(`[${clientName}] Shutdown complete.`);
        resolve();
      }
    }, 500);
  });
}

module.exports = { subscriberRoutine };