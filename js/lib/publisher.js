async function publisherRoutine(
  clientName,
  channels,
  mode,
  measureRTT,
  verbose,
  dataSize,
  client,
  isRunningRef,
  totalMessagesRef,
  rateLimiter
) {
  if (verbose) {
    console.log(
      `Publisher ${clientName} started. Mode: ${mode} | Channels: ${channels.length} | Payload: ${
        measureRTT ? 'RTT timestamp' : `fixed size ${dataSize} bytes`
      }`
    );
  }

  const payload = !measureRTT ? 'A'.repeat(dataSize) : '';
  const duplicatedClient = client.duplicate(); // Create a duplicated connection for this publisher

  try {
    while (isRunningRef.value) {
      for (const channel of channels) {
        try {
          // Apply rate limiting if configured
          if (rateLimiter) {
            await rateLimiter.removeTokens(1);
          }
          
          let msg = payload;
          if (measureRTT) {
            msg = Date.now().toString();
          }

          if (mode === 'spublish') {
            await duplicatedClient.spublish(channel, msg);
          } else {
            await duplicatedClient.publish(channel, msg);
          }
          totalMessagesRef.value++;
        } catch (err) {
          console.error(`Error publishing to channel ${channel}:`, err);
        }
      }
    }
  } finally {
    // Clean shutdown - disconnect the client
    if (verbose) {
      console.log(`Publisher ${clientName} shutting down...`);
    }
    try {
      duplicatedClient.disconnect();
      if (verbose) {
        console.log(`Publisher ${clientName} disconnected successfully`);
      }
    } catch (err) {
      console.error(`Error disconnecting publisher ${clientName}:`, err);
    }
  }
}

module.exports = { publisherRoutine };
