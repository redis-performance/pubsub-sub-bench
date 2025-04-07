function safeBigIntUs() {
  return process.hrtime.bigint() / 1000n;
}

async function subscriberRoutine(
  clientName,
  mode,
  channels,
  printMessages,
  reconnectInterval,
  measureRTT,
  client,
  isRunningRef,
  rttValues,
  totalMessagesRef,
  totalSubscribedRef,
  totalConnectsRef,
  verbose
) {
  let pubsub = null;

  const subscribe = async () => {
    if (pubsub) {
      if (channels.length > 1) {
        try {
          if (mode === 'ssubscribe') {
            await pubsub.sunsubscribe(...channels.slice(1));
            totalSubscribedRef.value -= channels.slice(1).length;
            pubsub = client.duplicate();
            await pubsub.ssubscribe(...channels.slice(1));
            totalSubscribedRef.value += channels.slice(1).length;
          } else {
            await pubsub.unsubscribe(...channels.slice(1));
            totalSubscribedRef.value -= channels.slice(1).length;
            pubsub = client.duplicate();
            await pubsub.subscribe(...channels.slice(1));
            totalSubscribedRef.value += channels.slice(1).length;
          }
          totalConnectsRef.value++;
        } catch (err) {
          console.error(`[${clientName}] Error during unsubscribe/subscribe:`, err);
        }
      } else {
        console.log(`[${clientName}] Only one channel, skipping re-subscribe`);
      }
    } else {
      pubsub = client.duplicate();
      pubsub.on('error', (err) => {
        console.error(`[${clientName}] Redis error: ${err.message}`);
      });

      if (mode === 'ssubscribe') {
        await pubsub.ssubscribe(...channels);
      } else {
        await pubsub.subscribe(...channels);
      }
      totalSubscribedRef.value += channels.length;
      totalConnectsRef.value++;
    }

    return pubsub;
  };

  pubsub = await subscribe();

  const handler = (channel, message) => {
    if (printMessages) {
      console.log(`[${clientName}] ${channel}: ${message}`);
    }

    if (measureRTT) {
      try {
        const now = BigInt(Date.now()) * 1000n;
        const timestamp = BigInt(message);
        const rtt = now - timestamp;
        rttValues.push(rtt);
        if (verbose) {
          console.log(`[${clientName}] RTT measured: ${rtt} µs (sent ${timestamp}, recv ${now})`);
        }
      } catch (err) {
        console.error(`[${clientName}] Invalid RTT message: ${message}`, err);
      }
    }

    totalMessagesRef.value++;
  };

  if (mode === 'ssubscribe') {
    pubsub.on('smessage', handler);
  } else {
    pubsub.on('message', handler);
  }

  let interval = null;
  if (reconnectInterval > 0) {
    interval = setInterval(async () => {
      if (isRunningRef.value) {
        await subscribe();
      } else {
        clearInterval(interval);
      }
    }, reconnectInterval);
  }

  // Handle graceful shutdown
  const shutdown = async () => {
    try {
      if (interval) clearInterval(interval);

      if (pubsub) {
        if (mode === 'ssubscribe') {
          await pubsub.sunsubscribe(...channels);
        } else {
          await pubsub.unsubscribe(...channels);
        }

        await pubsub.quit(); // graceful Redis shutdown
      }
    } catch (err) {
      console.warn(`[${clientName}] Error during shutdown:`, err.message);
    }
  };

  return new Promise((resolve) => {
    const check = setInterval(async () => {
      if (!isRunningRef.value) {
        clearInterval(check);
        await shutdown();
        resolve();
      }
    }, 1000);
  });
}

module.exports = { subscriberRoutine };
