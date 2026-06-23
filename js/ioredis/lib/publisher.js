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
  rateLimiter,
  skipDuplicate = false
) {
  if (verbose) {
    console.log(
      `Publisher ${clientName} started. Mode: ${mode} | Channels: ${channels.length} | Payload: ${
        measureRTT ? 'RTT timestamp' : `fixed size ${dataSize} bytes`
      }`
    );
  }

  // Pre-allocate payload once per publisher to avoid repeated allocations
  // Timestamp format: 13 bytes for milliseconds (e.g., "1730745600000")
  // Format: "<timestamp> <padding>" to reach dataSize
  const timestampSize = 13; // Date.now() returns milliseconds (13 digits)
  let paddingPayload = '';

  if (measureRTT && dataSize > timestampSize + 1) {
    // +1 for space separator
    const paddingSize = dataSize - timestampSize - 1;
    paddingPayload = 'A'.repeat(paddingSize);
  } else if (!measureRTT) {
    paddingPayload = 'A'.repeat(dataSize);
  }

  // For cluster mode, use the client directly without duplication
  // The cluster client handles routing and can be shared across publishers
  const duplicatedClient = skipDuplicate ? client : client.duplicate();

  // Wait for client to be ready
  if (!skipDuplicate) {
    await new Promise((resolve, reject) => {
      if (duplicatedClient.status === 'ready') {
        resolve();
      } else {
        duplicatedClient.once('ready', resolve);
        duplicatedClient.once('error', reject);
      }
    });
  }

  try {
    if (measureRTT) {
      // RTT mode: generate timestamp for each message with padding to reach dataSize
      while (isRunningRef.value) {
        for (const channel of channels) {
          try {
            // Apply rate limiting if configured
            if (rateLimiter) {
              await rateLimiter.removeTokens(1);
            }

            let msg;
            if (dataSize > timestampSize + 1) {
              // Format: "<timestamp> <padding>"
              msg = Date.now().toString() + ' ' + paddingPayload;
            } else {
              // Just timestamp if dataSize is too small
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
            process.exit(1);
          }
        }
      }
    } else {
      // Fixed payload mode: reuse pre-allocated payload
      while (isRunningRef.value) {
        for (const channel of channels) {
          try {
            // Apply rate limiting if configured
            if (rateLimiter) {
              await rateLimiter.removeTokens(1);
            }

            if (mode === 'spublish') {
              await duplicatedClient.spublish(channel, paddingPayload);
            } else {
              await duplicatedClient.publish(channel, paddingPayload);
            }
            totalMessagesRef.value++;
          } catch (err) {
            console.error(`Error publishing to channel ${channel}:`, err);
            process.exit(1);
          }
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
