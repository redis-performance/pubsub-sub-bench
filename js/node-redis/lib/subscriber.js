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
  async function handleMessage(channel, message) {
    if (printMessages) {
      console.log(`[${clientName}] Received message on channel ${channel}: ${message}`);
    }

    if (measureRTT) {
      const now = Date.now();
      const sentTime = parseInt(message, 10);
      const rtt = now - sentTime;
      rttAccumulator.add(rtt);
      rttHistogram.recordValue(rtt);
    }

    totalMessagesRef.value++;
  }

  async function subscribe() {
    try {
      if (mode === 'ssubscribe') {
        await client.sSubscribe(channels, handleMessage);
      } else {
        await client.subscribe(channels, handleMessage);
      }

      totalSubscribedRef.value += channels.length;
      totalConnectsRef.value++;

      if (verbose) {
        console.log(`${clientName} subscribed to ${channels.length} channels`);
      }
    } catch (err) {
      console.error(`Error in subscribe for ${clientName}:`, err);
      return false;
    }
    return true;
  }

  async function unsubscribe() {
    try {
      if (mode === 'ssubscribe') {
        await client.sUnsubscribe(channels);
      } else {
        await client.unsubscribe(channels);
      }

      totalSubscribedRef.value -= channels.length;

      if (verbose) {
        console.log(`${clientName} unsubscribed from ${channels.length} channels`);
      }
    } catch (err) {
      console.error(`Error in unsubscribe for ${clientName}:`, err);
    }
  }

  if (verbose) {
    console.log(
      `Subscriber ${clientName} starting. Mode: ${mode} | Channels: ${channels.length}`
    );
  }

  try {
    while (isRunningRef.value) {
      const subscribed = await subscribe();
      if (!subscribed) {
        console.error(`${clientName} failed to subscribe, retrying...`);
        continue;
      }

      if (reconnectInterval > 0) {
        await new Promise(resolve => setTimeout(resolve, reconnectInterval));
        if (isRunningRef.value) {
          await unsubscribe();
        }
      } else {
        // If no reconnect interval, just wait until shutdown
        await new Promise(resolve => {
          const checkInterval = setInterval(() => {
            if (!isRunningRef.value) {
              clearInterval(checkInterval);
              resolve();
            }
          }, 100);
        });
      }
    }
  } finally {
    if (verbose) {
      console.log(`Subscriber ${clientName} shutting down...`);
    }
    await unsubscribe();
  }
}

module.exports = { subscriberRoutine };