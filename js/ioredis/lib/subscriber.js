async function subscriberRoutine(
  clientName,
  mode,
  channels,
  printMessages,
  reconnectInterval,
  measureRTT,
  client,
  isRunningRef,
  rttAccumulator,
  rttHistogram,
  totalMessagesRef,
  totalSubscribedRef,
  totalConnectsRef,
  verbose,
  totalClients
) {
  let pubsub = null;
  let reconnectTimer = null;

  // Subscribe function which creates a new duplicated connection.
  const subscribe = async () => {
    try {
      // If already subscribed, try unsubscribing extra channels first.
      if (pubsub) {
        if (channels.length > 1) {
          if (mode === 'ssubscribe') {
            await pubsub.sunsubscribe(...channels.slice(1));
          } else {
            await pubsub.unsubscribe(...channels.slice(1));
          }
          totalSubscribedRef.value -= channels.slice(1).length;
        }
        // Duplicate connection afresh.
        pubsub = client.duplicate();
      } else {
        pubsub = client.duplicate();
      }

      // Set up error logging.
      pubsub.on('error', (err) => {
        console.error(`[${clientName}] Redis error: ${err.message}`);
      });

      // Subscribe to channels with appropriate method.
      if (mode === 'ssubscribe') {
        await pubsub.ssubscribe(...channels);
        pubsub.on('smessage', handleMessage);
      } else {
        await pubsub.subscribe(...channels);
        pubsub.on('message', handleMessage);
      }

      totalSubscribedRef.value += channels.length;
      totalConnectsRef.value++;
    } catch (err) {
      console.error(`[${clientName}] Subscribe error:`, err);
    }
  };

  // Handler for incoming messages.
  const handleMessage = (channel, message) => {
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

  // Initial subscription.
  await subscribe();

  // Set up automatic re-subscription if reconnectInterval is set.
  if (reconnectInterval > 0) {
    reconnectTimer = setInterval(async () => {
      if (isRunningRef.value) {
        await subscribe();
      }
    }, reconnectInterval);
  }

  // Shutdown function with a forced timeout safeguard.
  const shutdown = () => {
    return new Promise(async (resolve) => {
      // Clear the reconnection timer if set.
      if (reconnectTimer) clearInterval(reconnectTimer);

      // Set a timeout in case shutdown hangs.
      const forcedTimeout = setTimeout(() => {
        console.warn(`[${clientName}] Shutdown timed out. Forcing resolve.`);
        resolve();
      }, 5000); // 5 seconds fallback

      // Attempt to unsubscribe from channels.
      try {
        if (pubsub) {
          if (mode === 'ssubscribe') {
            await pubsub.sunsubscribe(...channels);
          } else {
            await pubsub.unsubscribe(...channels);
          }
        }
      } catch (err) {
        console.warn(`[${clientName}] Unsubscribe error: ${err.message}`);
      }

      // Now disconnect immediately.
      try {
        if (pubsub) {
          pubsub.disconnect();
        }
      } catch (err) {
        console.warn(`[${clientName}] Disconnect error: ${err.message}`);
      }

      clearTimeout(forcedTimeout);
      resolve();
    });
  };

  // Return a promise that waits until isRunningRef becomes false, then cleans up.
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
