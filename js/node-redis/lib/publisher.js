async function publisherRoutine(
  clientName,
  channels,
  mode,
  measureRTT,
  verbose,
  dataSize,
  client,
  isRunningRef,
  totalMessagesRef
) {
  await client.connect()
  if (verbose) {
    console.log(
      `Publisher ${clientName} started. Mode: ${mode} | Channels: ${channels.length} | Payload: ${
        measureRTT ? 'RTT timestamp' : `fixed size ${dataSize} bytes`
      }`
    );
  }

  const payload = !measureRTT ? 'A'.repeat(dataSize) : '';

  try {
    while (isRunningRef.value) {
      for (const channel of channels) {
        try {
          let msg = payload;
          if (measureRTT) {
            msg = Date.now().toString();
          }

          if (mode === 'spublish') {
            await client.sPublish(channel, msg);
          } else {
            await client.publish(channel, msg);
          }
          totalMessagesRef.value++;
        } catch (err) {
          console.error(`Error publishing to channel ${channel}:`, err);
        }
      }
    }
  } finally {
    // Clean shutdown - client is managed by redisManager
    if (verbose) {
      console.log(`Publisher ${clientName} shutting down...`);
    }
  }
}

module.exports = { publisherRoutine };