const fs = require('fs');
const hdr = require('hdr-histogram-js');

// Simple accumulator for RTT stats per tick
class RttAccumulator {
  constructor() {
    this.reset();
  }

  reset() {
    this.sum = 0;
    this.count = 0;
  }

  add(value) {
    this.sum += value;
    this.count++;
  }

  getAverage() {
    return this.count > 0 ? this.sum / this.count : null;
  }
}

function createRttHistogram() {
  return hdr.build({
    lowestDiscernibleValue: 1,
    highestTrackableValue: 10_000_000,
    numberOfSignificantValueDigits: 3
  });
}

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
  rttAccumulator,
  rttHistogram
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
        if (rttAccumulator.count > 0) {
          avgRttMs = rttAccumulator.getAverage();
          metrics.push(avgRttMs.toFixed(3));
          // Reset accumulator after using the values
          rttAccumulator.reset();
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
  rttHistogram,
  perSecondStats
) {
  const duration = (end - start);
  const messageRate = totalMessages / duration;

  console.log('#################################################');
  console.log(`Mode: ${mode}`);
  console.log(`Total Duration: ${duration.toFixed(6)} Seconds`);
  console.log(`Message Rate: ${messageRate.toFixed(6)} msg/sec`);

  const result = {
    StartTime: Math.floor(start),
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

  if (argv['measure-rtt-latency'] && !mode.includes('publish')) {
    const avgRtt = rttHistogram.mean;
    const p50 = rttHistogram.getValueAtPercentile(50);
    const p95 = rttHistogram.getValueAtPercentile(95);
    const p99 = rttHistogram.getValueAtPercentile(99);
    const p999 = rttHistogram.getValueAtPercentile(99.9);

    result.RTTSummary = {
      AvgMs: Number(avgRtt.toFixed(3)),
      P50Ms: Number(p50.toFixed(3)),
      P95Ms: Number(p95.toFixed(3)),
      P99Ms: Number(p99.toFixed(3)),
      P999Ms: Number(p999.toFixed(3)),
      totalCount: rttHistogram.totalCount
    };

    console.log(`Avg  RTT       ${avgRtt.toFixed(3)} ms`);
    console.log(`P50  RTT       ${p50.toFixed(3)} ms`);
    console.log(`P95  RTT       ${p95.toFixed(3)} ms`);
    console.log(`P99  RTT       ${p99.toFixed(3)} ms`);
    console.log(`P999 RTT       ${p999.toFixed(3)} ms`);
    console.log(`Total Messages tracked latency      ${rttHistogram.totalCount} messages`);
  }

  console.log('#################################################');

  if (argv['json-out-file']) {
    fs.writeFileSync(argv['json-out-file'], JSON.stringify(result, null, 2));
    console.log(`Results written to ${argv['json-out-file']}`);
  }
}

module.exports = {
  updateCLI,
  writeFinalResults,
  createRttHistogram,
  RttAccumulator
};
