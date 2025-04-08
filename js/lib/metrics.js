const fs = require('fs');
const hdr = require('hdr-histogram-js');

function formatRow(row) {
  const widths = [6, 15, 14, 14, 22, 14];
  return row.map((val, i) => String(val).padEnd(widths[i] || 10)).join('');
}

function updateCLI(
  updateInterval,
  messageLimit,
  testTime,
  measureRTT,
  mode,
  isRunningRef,
  totalMessagesRef,
  totalConnectsRef,
  totalSubscribedRef,
  totalPublishersRef,
  messageRateTs,
  rttValues
) {
  return new Promise((resolve) => {
    let prevTime = Date.now();
    let prevMessageCount = 0;
    let prevConnectCount = 0;
    let startTime = Date.now();
    let resolved = false;

    console.log('Starting benchmark...');

    const header = ['Time', 'Total Messages', 'Message Rate', 'Connect Rate'];
    header.push(mode.includes('subscribe') ? 'Active Subscriptions' : 'Active Publishers');
    if (measureRTT) header.push('Avg RTT (ms)');
    console.log(formatRow(header));
    const perSecondStats = [];

    const interval = setInterval(() => {
      const now = Date.now();
      const elapsed = (now - prevTime) / 1000;

      const messageRate = (totalMessagesRef.value - prevMessageCount) / elapsed;
      const connectRate = (totalConnectsRef.value - prevConnectCount) / elapsed;

      if (prevMessageCount === 0 && totalMessagesRef.value !== 0) {
        startTime = Date.now();
      }

      if (totalMessagesRef.value !== 0) {
        messageRateTs.push(messageRate);
      }

      prevMessageCount = totalMessagesRef.value;
      prevConnectCount = totalConnectsRef.value;
      prevTime = now;

      const metrics = [
        Math.floor((now - startTime) / 1000),
        totalMessagesRef.value,
        messageRate.toFixed(2),
        connectRate.toFixed(2),
        mode.includes('subscribe') ? totalSubscribedRef.value : totalPublishersRef.value
      ];

      let avgRttMs = null;

      if (measureRTT) {
        const tickRttValues = rttValues.splice(0);
        if (tickRttValues.length > 0) {
          const sum = tickRttValues.reduce((a, b) => a + b, 0n);
          const avgRtt = Number(sum) / tickRttValues.length;
          avgRttMs = avgRtt / 1000;
          metrics.push(avgRttMs.toFixed(3));
        } else {
          metrics.push('--');
        }
      }

      perSecondStats.push({
        second: Math.floor((now - startTime) / 1000),
        messages: totalMessagesRef.value,
        messageRate: Number(messageRate.toFixed(2)),
        avgRttMs: avgRttMs !== null ? Number(avgRttMs.toFixed(3)) : null
      });

      console.log(formatRow(metrics));

      const shouldStop =
        (messageLimit > 0 && totalMessagesRef.value >= messageLimit) ||
        (testTime > 0 && now - startTime >= testTime * 1000 && totalMessagesRef.value !== 0);

      if (shouldStop && !resolved) {
        resolved = true;
        clearInterval(interval);
        isRunningRef.value = false;
        resolve({ startTime, now, perSecondStats });
      }
    }, updateInterval * 1000);

    process.on('SIGINT', () => {
      if (!resolved) {
        console.log('\nReceived Ctrl-C - shutting down');
        clearInterval(interval);
        isRunningRef.value = false;
        resolved = true;
        resolve({ startTime, now: Date.now(), perSecondStats, sigint: true });
      }
    });
  });
}

function writeFinalResults(
  start,
  end,
  argv,
  mode,
  totalMessages,
  totalSubscribed,
  messageRateTs,
  rttValues,
  perSecondStats
) {
  const duration = (end - start) / 1000;
  const messageRate = totalMessages / duration;

  console.log('#################################################');
  console.log(`Mode: ${mode}`);
  console.log(`Total Duration: ${duration.toFixed(6)} Seconds`);
  console.log(`Message Rate: ${messageRate.toFixed(6)} msg/sec`);

  if (argv['measure-rtt-latency'] && !mode.includes('publish')) {
    const histogram = hdr.build({
      lowestDiscernibleValue: 1,
      highestTrackableValue: 10_000_000,
      numberOfSignificantValueDigits: 3
    });

    rttValues.forEach((rtt) => {
      const val = Number(rtt);
      if (val >= 0) histogram.recordValue(val);
    });

    console.log(`Avg RTT       ${(histogram.mean / 1000).toFixed(3)} ms`);
    console.log(`P50 RTT       ${(histogram.getValueAtPercentile(50) / 1000).toFixed(3)} ms`);
    console.log(`P95 RTT       ${(histogram.getValueAtPercentile(95) / 1000).toFixed(3)} ms`);
    console.log(`P99 RTT       ${(histogram.getValueAtPercentile(99) / 1000).toFixed(3)} ms`);
    console.log(`P999 RTT      ${(histogram.getValueAtPercentile(99.9) / 1000).toFixed(3)} ms`);
  }

  console.log('#################################################');

  if (argv['json-out-file']) {
    const result = {
      StartTime: Math.floor(start / 1000),
      Duration: duration,
      Mode: mode,
      MessageRate: messageRate,
      TotalMessages: totalMessages,
      TotalSubscriptions: totalSubscribed,
      ChannelMin: argv['channel-minimum'],
      ChannelMax: argv['channel-maximum'],
      SubscribersPerChannel: argv['subscribers-per-channel'],
      MessagesPerChannel: argv['messages'],
      MessageRateTs: messageRateTs,
      OSSDistributedSlots: argv['oss-cluster-api-distribute-subscribers'],
      Addresses: [`${argv.host}:${argv.port}`],
      PerSecondStats: perSecondStats
    };
    fs.writeFileSync(argv['json-out-file'], JSON.stringify(result, null, 2));
    console.log(`Results written to ${argv['json-out-file']}`);
  }
}

module.exports = {
  updateCLI,
  writeFinalResults
};
