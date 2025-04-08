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
  if (verbose) {
    console.log(
      `Publisher ${clientName} started. Mode: ${mode} | Channels: ${channels.length} | Payload: ${
        measureRTT ? 'RTT timestamp' : `fixed size ${dataSize} bytes`
      }`
    );
  }

  const payload = !measureRTT ? 'A'.repeat(dataSize) : '';

  while (isRunningRef.value) {
    let msg = payload;
    if (measureRTT) {
      msg = process.hrtime.bigint() / 1000;
    }

    for (const channel of channels) {
      try {
        if (mode === 'spublish') {
          await client.spublish(channel, msg);
        } else {
          await client.publish(channel, msg);
        }
        totalMessagesRef.value++;
      } catch (err) {
        console.error(`Error publishing to channel ${channel}:`, err);
      }
    }
  }
}

module.exports = { publisherRoutine };
