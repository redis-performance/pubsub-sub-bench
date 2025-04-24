// filepath: /Users/hristo.temelski/code/etc/pubsub-sub-bench/js/node-redis/lib/publisher.js
const { createClient } = require('redis');

async function publisherRoutine(
  clientName,
  channels,
  mode,
  measureRTT,
  verbose,
  dataSize,
  redisOptions,
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
  
  // Create a new Redis client
  const client = createClient(redisOptions);
  
  // Set up error handling
  client.on('error', (err) => {
    console.error(`[${clientName}] Redis error: ${err.message}`);
  });
  
  try {
    // Connect to Redis
    await client.connect();
    
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
            await client.sPublish(channel, msg);
          } else {
            await client.publish(channel, msg);
          }
          totalMessagesRef.value++;
        } catch (err) {
          console.error(`[${clientName}] Error publishing to channel ${channel}:`, err);
        }
      }
    }
  } catch (err) {
    console.error(`[${clientName}] Redis connection error:`, err);
  } finally {
    // Clean shutdown - disconnect the client
    if (verbose) {
      console.log(`Publisher ${clientName} shutting down...`);
    }
    try {
      await client.quit();
      if (verbose) {
        console.log(`Publisher ${clientName} disconnected successfully`);
      }
    } catch (err) {
      console.error(`Error disconnecting publisher ${clientName}:`, err);
    }
  }
}

module.exports = { publisherRoutine };