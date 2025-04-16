package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
	redis "github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

const (
	redisPoolSize              = "pool_size"
	redisPoolSizeDefault       = 0
	redisTLSCA                 = "tls_ca"
	redisTLSCert               = "tls_cert"
	redisTLSKey                = "tls_key"
	redisTLSInsecureSkipVerify = "tls_insecure_skip_verify"
)

const Inf = rate.Limit(math.MaxFloat64)

var totalMessages uint64
var totalSubscribedChannels int64
var totalPublishers int64
var totalConnects uint64
var clusterSlicesMu sync.Mutex

type testResult struct {
	StartTime             int64     `json:"StartTime"`
	Duration              float64   `json:"Duration"`
	Mode                  string    `json:"Mode"`
	MessageRate           float64   `json:"MessageRate"`
	TotalMessages         uint64    `json:"TotalMessages"`
	TotalSubscriptions    int       `json:"TotalSubscriptions"`
	ChannelMin            int       `json:"ChannelMin"`
	ChannelMax            int       `json:"ChannelMax"`
	SubscribersPerChannel int       `json:"SubscribersPerChannel"`
	MessagesPerChannel    int64     `json:"MessagesPerChannel"`
	MessageRateTs         []float64 `json:"MessageRateTs"`
	OSSDistributedSlots   bool      `json:"OSSDistributedSlots"`
	Addresses             []string  `json:"Addresses"`
}

func publisherRoutine(clientName string, channels []string, mode string, measureRTT bool, verbose bool, dataSize int, ctx context.Context, wg *sync.WaitGroup, client *redis.Client, useLimiter bool, rateLimiter *rate.Limiter) {
	defer wg.Done()

	if verbose {
		log.Printf("Publisher %s started. Mode: %s | Channels: %d | Payload: %s",
			clientName, mode, len(channels),
			func() string {
				if measureRTT {
					return "RTT timestamp"
				}
				return fmt.Sprintf("fixed size %d bytes", dataSize)
			}(),
		)
	}

	var payload string
	if !measureRTT {
		payload = strings.Repeat("A", dataSize)
	}

	for {
		select {
		case <-ctx.Done():
			log.Printf("Publisher %s exiting due to context cancellation.", clientName)
			return

		default:
			msg := payload

			for _, ch := range channels {
				if useLimiter {
					r := rateLimiter.ReserveN(time.Now(), int(1))
					time.Sleep(r.Delay())
				}
				if measureRTT {
					now := time.Now().UnixMilli()
					msg = strconv.FormatInt(int64(now), 10)
				}
				var err error
				switch mode {
				case "spublish":
					err = client.SPublish(ctx, ch, msg).Err()
				default:
					err = client.Publish(ctx, ch, msg).Err()
				}
				if err != nil {
					log.Printf("Error publishing to channel %s: %v", ch, err)
				}
				atomic.AddUint64(&totalMessages, 1)
			}
		}
	}
}

func subscriberRoutine(clientName, mode string, channels []string, verbose bool, connectionReconnectInterval int, measureRTT bool, rttLatencyChannel chan int64, ctx context.Context, wg *sync.WaitGroup, client *redis.Client) { // Tell the caller we've stopped
	defer wg.Done()
	var reconnectTicker *time.Ticker
	if connectionReconnectInterval > 0 {
		reconnectTicker = time.NewTicker(time.Duration(connectionReconnectInterval) * time.Millisecond)
		defer reconnectTicker.Stop()
	} else {
		reconnectTicker = time.NewTicker(1 * time.Second)
		reconnectTicker.Stop()
	}

	var pubsub *redis.PubSub
	nChannels := len(channels)

	// Helper function to handle subscription based on mode
	subscribe := func() {
		if pubsub != nil {
			if nChannels > 1 {
				// Unsubscribe based on mode before re-subscribing
				if mode == "ssubscribe" {
					if err := pubsub.SUnsubscribe(ctx, channels[1:]...); err != nil {
						fmt.Printf("Error during SUnsubscribe: %v\n", err)
					}
					pubsub.Close()
					atomic.AddInt64(&totalSubscribedChannels, int64(-len(channels[1:])))
					pubsub = client.SSubscribe(ctx, channels[1:]...)
					atomic.AddInt64(&totalSubscribedChannels, int64(len(channels[1:])))
				} else {
					if err := pubsub.Unsubscribe(ctx, channels[1:]...); err != nil {
						fmt.Printf("Error during Unsubscribe: %v\n", err)
						pubsub.Close()
						atomic.AddInt64(&totalSubscribedChannels, int64(-len(channels[1:])))
						pubsub = client.Subscribe(ctx, channels[1:]...)
						atomic.AddInt64(&totalSubscribedChannels, int64(len(channels[1:])))
					}
				}
				atomic.AddUint64(&totalConnects, 1)
			} else {
				log.Println(fmt.Sprintf("Skipping (S)UNSUBSCRIBE given client %s had only one channel subscribed in this connection: %v.", clientName, channels))
			}
		} else {
			switch mode {
			case "ssubscribe":
				pubsub = client.SSubscribe(ctx, channels...)
			default:
				pubsub = client.Subscribe(ctx, channels...)
			}
			atomic.AddInt64(&totalSubscribedChannels, int64(len(channels)))
			atomic.AddUint64(&totalConnects, 1)
		}

	}

	subscribe()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, exit routine
			if pubsub != nil {
				if mode == "ssubscribe" {
					_ = pubsub.SUnsubscribe(ctx, channels...)
				} else {
					_ = pubsub.Unsubscribe(ctx, channels...)
				}
				pubsub.Close()
			}
			return
		case <-reconnectTicker.C:
			// Reconnect interval triggered, unsubscribe and resubscribe
			if reconnectTicker != nil {
				subscribe()
			}
		default:
			// Handle messages
			msg, err := pubsub.ReceiveMessage(ctx)
			if err != nil {
				// Handle Redis connection errors, e.g., reconnect immediately
				if err == redis.Nil || err == context.DeadlineExceeded || err == context.Canceled {
					continue
				}
				panic(err)
			}
			if verbose {
				log.Println(fmt.Sprintf("received message in channel %s. Message: %s", msg.Channel, msg.Payload))
			}
			if measureRTT {
				if ts, err := strconv.ParseInt(msg.Payload, 10, 64); err == nil {
					now := time.Now().UnixMicro()
					rtt := now - ts
					rttLatencyChannel <- rtt
					if verbose {
						log.Printf("RTT measured: %d ms\n", rtt/1000)
					}
				} else {
					log.Printf("Invalid timestamp in message: %s, err: %v\n", msg.Payload, err)
				}
			}
			atomic.AddUint64(&totalMessages, 1)
		}
	}
}

func main() {
	host := flag.String("host", "127.0.0.1", "redis host.")
	port := flag.String("port", "6379", "redis port.")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to file")
	rps := flag.Int64("rps", 0, "Max rps. If 0 no limit is applied and the DB is stressed up to maximum.")
	rpsburst := flag.Int64("rps-burst", 0, "Max rps burst. If 0 the allowed burst will be the ammount of clients.")
	password := flag.String("a", "", "Password for Redis Auth.")
	dataSize := flag.Int("data-size", 128, "Payload size in bytes for publisher messages when RTT mode is disabled")
	mode := flag.String("mode", "subscribe", "Subscribe mode. Either 'subscribe' or 'ssubscribe'.")
	username := flag.String("user", "", "Used to send ACL style 'AUTH username pass'. Needs -a.")
	subscribers_placement := flag.String("subscribers-placement-per-channel", "dense", "(dense,sparse) dense - Place all subscribers to channel in a specific shard. sparse- spread the subscribers across as many shards possible, in a round-robin manner.")
	channel_minimum := flag.Int("channel-minimum", 1, "channel ID minimum value ( each channel has a dedicated thread ).")
	channel_maximum := flag.Int("channel-maximum", 100, "channel ID maximum value ( each channel has a dedicated thread ).")
	subscribers_per_channel := flag.Int("subscribers-per-channel", 1, "number of subscribers per channel.")
	clients := flag.Int("clients", 50, "Number of parallel connections.")
	min_channels_per_subscriber := flag.Int("min-number-channels-per-subscriber", 1, "min number of channels to subscribe to, per connection.")
	max_channels_per_subscriber := flag.Int("max-number-channels-per-subscriber", 1, "max number of channels to subscribe to, per connection.")
	min_reconnect_interval := flag.Int("min-reconnect-interval", 0, "min reconnect interval. if 0 disable (s)unsubscribe/(s)ubscribe.")
	max_reconnect_interval := flag.Int("max-reconnect-interval", 0, "max reconnect interval. if 0 disable (s)unsubscribe/(s)ubscribe.")
	messages_per_channel_subscriber := flag.Int64("messages", 0, "Number of total messages per subscriber per channel.")
	json_out_file := flag.String("json-out-file", "", "Name of json output file, if not set, will not print to json.")
	client_update_tick := flag.Int("client-update-tick", 1, "client update tick.")
	test_time := flag.Int("test-time", 0, "Number of seconds to run the test, after receiving the first message.")
	randSeed := flag.Int64("rand-seed", 12345, "Random deterministic seed.")
	subscribe_prefix := flag.String("subscriber-prefix", "channel-", "prefix for subscribing to channel, used in conjunction with key-minimum and key-maximum.")
	client_output_buffer_limit_pubsub := flag.String("client-output-buffer-limit-pubsub", "", "Specify client output buffer limits for clients subscribed to at least one pubsub channel or pattern. If the value specified is different that the one present on the DB, this setting will apply.")
	distributeSubscribers := flag.Bool("oss-cluster-api-distribute-subscribers", false, "read cluster slots and distribute subscribers among them.")
	printMessages := flag.Bool("print-messages", false, "print messages.")
	verbose := flag.Bool("verbose", false, "verbose print.")
	measureRTT := flag.Bool("measure-rtt-latency", false, "Enable RTT latency measurement mode")
	version := flag.Bool("version", false, "print version and exit.")
	timeout := flag.Duration("redis-timeout", time.Second*30, "determines the timeout to pass to redis connection setup. It adjust the connection, read, and write timeouts.")
	poolSizePtr := flag.Int(redisPoolSize, redisPoolSizeDefault, "Maximum number of socket connections per node.")
	resp := flag.Int("resp", 2, "redis command response protocol (2 - RESP 2, 3 - RESP 3)")
	flag.Parse()

	git_sha := toolGitSHA1()
	git_dirty_str := ""
	if toolGitDirty() {
		git_dirty_str = "-dirty"
	}
	if *version {
		fmt.Fprintf(os.Stdout, "pubsub-sub-bench (git_sha1:%s%s)\n", git_sha, git_dirty_str)
		os.Exit(0)
	}

	if *test_time != 0 && *messages_per_channel_subscriber != 0 {
		log.Fatal(fmt.Errorf("--messages and --test-time are mutially exclusive ( please specify one or the other )"))
	}
	log.Println(fmt.Sprintf("pubsub-sub-bench (git_sha1:%s%s)", git_sha, git_dirty_str))
	log.Println(fmt.Sprintf("using random seed:%d", *randSeed))
	rand.Seed(*randSeed)
	if *measureRTT {
		log.Println("RTT measurement enabled.")
	} else {
		log.Println("RTT measurement disabled.")
	}
	if *verbose {
		log.Println("verbose mode enabled.")
	}
	ctx := context.Background()
	nodeCount := 0
	var nodesAddresses []string
	var nodeClients []*redis.Client
	poolSize := *poolSizePtr
	var clusterClient *redis.ClusterClient
	var standaloneClient *redis.Client

	standaloneOptions := redis.Options{Protocol: *resp,
		Addr:         fmt.Sprintf("%s:%s", *host, *port),
		Username:     *username,
		Password:     *password,
		DialTimeout:  *timeout,
		ReadTimeout:  *timeout,
		WriteTimeout: *timeout,
		PoolSize:     poolSize,
	}
	clusterOptions := redis.ClusterOptions{Protocol: *resp,
		Addrs:        []string{fmt.Sprintf("%s:%s", *host, *port)},
		Username:     *username,
		Password:     *password,
		DialTimeout:  *timeout,
		ReadTimeout:  *timeout,
		WriteTimeout: *timeout,
		PoolSize:     poolSize,
	}

	if *distributeSubscribers {
		clusterClient = redis.NewClusterClient(&clusterOptions)
		// ReloadState reloads cluster state. It calls ClusterSlots func
		// to get cluster slots information.
		log.Println("Reloading cluster state.")
		clusterClient.ReloadState(ctx)
		err := clusterClient.Ping(ctx).Err()
		if err != nil {
			log.Fatal(err)
		}
		nodeCount, nodeClients, nodesAddresses = updateSecondarySlicesCluster(clusterClient, ctx)
	} else {
		nodeCount = 1
		nodesAddresses = append(nodesAddresses, fmt.Sprintf("%s:%s", *host, *port))
		standaloneClient = redis.NewClient(&standaloneOptions)
		err := standaloneClient.Ping(ctx).Err()
		if err != nil {
			log.Fatal(err)
		}
		nodeClients = append(nodeClients, standaloneClient)
	}
	//
	if strings.Compare(*client_output_buffer_limit_pubsub, "") != 0 {
		log.Println("client-output-buffer-limit-pubsub is not being enforced currently.")
	}

	totalMessages = 0
	total_channels := *channel_maximum - *channel_minimum + 1
	total_subscriptions := total_channels * *subscribers_per_channel
	total_messages := int64(total_subscriptions) * *messages_per_channel_subscriber
	subscriptions_per_node := total_subscriptions / nodeCount

	log.Println(fmt.Sprintf("Will use a subscriber prefix of: %s<channel id>", *subscribe_prefix))
	log.Println(fmt.Sprintf("total_channels: %d", total_channels))

	if *poolSizePtr == 0 {
		poolSize = subscriptions_per_node
		log.Println(fmt.Sprintf("Setting per Node pool size of %d given you haven't specified a value and we have %d Subscriptions per node. You can control this option via --%s=<value>", poolSize, subscriptions_per_node, redisPoolSize))
		clusterOptions.PoolSize = poolSize
		if *distributeSubscribers {
			log.Println("Reloading cluster state given we've changed pool size.")
			clusterClient = redis.NewClusterClient(&clusterOptions)
			// ReloadState reloads cluster state. It calls ClusterSlots func
			// to get cluster slots information.
			clusterClient.ReloadState(ctx)
			err := clusterClient.Ping(ctx).Err()
			if err != nil {
				log.Fatal(err)
			}
			nodeCount, nodeClients, nodesAddresses = updateSecondarySlicesCluster(clusterClient, ctx)
		}

	}

	log.Println(fmt.Sprintf("Detailing final setup used for benchmark."))
	log.Println(fmt.Sprintf("\tTotal nodes: %d", nodeCount))
	for i, nodeAddress := range nodesAddresses {
		log.Println(fmt.Sprintf("\tnode #%d: Address: %s", i, nodeAddress))
		log.Println(fmt.Sprintf("\t\tClient struct: %v", nodeClients[i]))
	}
	// trap Ctrl+C and call cancel on the context
	// We Use this instead of the previous stopChannel + chan radix.PubSubMessage
	ctx, cancel := context.WithCancel(ctx)
	cS := make(chan os.Signal, 1)
	signal.Notify(cS, os.Interrupt)
	defer func() {
		signal.Stop(cS)
		cancel()
	}()
	go func() {
		select {
		case <-cS:
			cancel()
		case <-ctx.Done():
		}
	}()

	// a WaitGroup for the goroutines to tell us they've stopped
	wg := sync.WaitGroup{}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
	}
	rttLatencyChannel := make(chan int64, 100000) // Channel for RTT measurements. buffer of 100K messages to process
	totalCreatedClients := 0
	if strings.Contains(*mode, "publish") {
		var requestRate = Inf
		var requestBurst = int(*rps)
		useRateLimiter := false
		if *rps != 0 {
			requestRate = rate.Limit(*rps)
			log.Println(fmt.Sprintf("running publisher mode with rate-limit enabled. Max published %d messages/sec.\n", *rps))
			useRateLimiter = true
			if *rpsburst != 0 {
				requestBurst = int(*rpsburst)
			}
		} else {
			log.Println(fmt.Sprintf("running publisher mode with maximum rate enabled."))
		}

		var rateLimiter = rate.NewLimiter(requestRate, requestBurst)
		// Only run publishers
		for client_id := 1; client_id <= *clients; client_id++ {
			channels := []string{}
			n_channels_this_pub := 0
			if *max_channels_per_subscriber == *min_channels_per_subscriber {
				n_channels_this_pub = *max_channels_per_subscriber
			} else {
				n_channels_this_pub = rand.Intn(*max_channels_per_subscriber-*min_channels_per_subscriber) + *min_channels_per_subscriber
			}
			for i := 0; i < n_channels_this_pub; i++ {
				new_channel_id := rand.Intn(*channel_maximum-*channel_minimum+1) + *channel_minimum
				new_channel := fmt.Sprintf("%s%d", *subscribe_prefix, new_channel_id)
				channels = append(channels, new_channel)
			}

			publisherName := fmt.Sprintf("publisher#%d", client_id)
			var client *redis.Client
			var err error

			ctx = context.Background()
			if strings.Compare(*mode, "spublish") == 0 && *distributeSubscribers {
				firstChannel := channels[0]
				client, err = clusterClient.MasterForKey(ctx, firstChannel)
				if err != nil {
					log.Fatal(err)
				}
			} else {
				nodePos := client_id % nodeCount
				client = nodeClients[nodePos]
				err = client.Ping(ctx).Err()
				if err != nil {
					log.Fatal(err)
				}
			}

			if *verbose {
				log.Printf("publisher %d targeting channels %v", client_id, channels)
			}

			wg.Add(1)
			go publisherRoutine(publisherName, channels, *mode, *measureRTT, *verbose, *dataSize, ctx, &wg, client, useRateLimiter, rateLimiter)
			atomic.AddInt64(&totalPublishers, 1)
			atomic.AddUint64(&totalConnects, 1)
		}

	} else if strings.Contains(*mode, "subscribe") {
		// Only run subscribers
		if strings.Compare(*subscribers_placement, "dense") == 0 {
			for client_id := 1; client_id <= *clients; client_id++ {
				channels := []string{}
				n_channels_this_conn := 0
				if *max_channels_per_subscriber == *min_channels_per_subscriber {
					n_channels_this_conn = *max_channels_per_subscriber
				} else {
					n_channels_this_conn = rand.Intn(*max_channels_per_subscriber-*min_channels_per_subscriber) + *min_channels_per_subscriber
				}
				for channel_this_conn := 1; channel_this_conn <= n_channels_this_conn; channel_this_conn++ {
					new_channel_id := rand.Intn(*channel_maximum-*channel_minimum+1) + *channel_minimum
					new_channel := fmt.Sprintf("%s%d", *subscribe_prefix, new_channel_id)
					channels = append(channels, new_channel)
				}
				totalCreatedClients++
				subscriberName := fmt.Sprintf("subscriber#%d", client_id)
				var client *redis.Client
				var err error = nil
				ctx = context.Background()

				if strings.Compare(*mode, "ssubscribe") == 0 && *distributeSubscribers == true {
					firstChannel := channels[0]
					client, err = clusterClient.MasterForKey(ctx, firstChannel)
					if err != nil {
						log.Fatal(err)
					}
				} else {
					nodes_pos := client_id % nodeCount
					addr := nodesAddresses[nodes_pos]
					client = nodeClients[nodes_pos]
					if *verbose {
						log.Printf("client %d is a STANDALONE client connected to node %d (address %s). Subscriber name %s", totalCreatedClients, nodes_pos, addr, subscriberName)
					}
					err = client.Ping(ctx).Err()
					if err != nil {
						log.Fatal(err)
					}
				}

				connectionReconnectInterval := 0
				if *max_reconnect_interval == *min_reconnect_interval {
					connectionReconnectInterval = *max_reconnect_interval
				} else {
					connectionReconnectInterval = rand.Intn(*max_reconnect_interval-*min_reconnect_interval) + *min_reconnect_interval
				}

				if connectionReconnectInterval > 0 {
					log.Printf("Using reconnection interval of %d milliseconds for subscriber: %s", connectionReconnectInterval, subscriberName)
				}
				if *verbose {
					log.Printf("subscriber: %s. Total channels %d: %v", subscriberName, len(channels), channels)
				}
				if totalCreatedClients%100 == 0 {
					log.Printf("Created %d clients so far.", totalCreatedClients)
				}
				wg.Add(1)
				go subscriberRoutine(subscriberName, *mode, channels, *printMessages, connectionReconnectInterval, *measureRTT, rttLatencyChannel, ctx, &wg, client)
			}
		}
	} else {
		log.Fatalf("Invalid mode '%s'. Must be one of: subscribe, ssubscribe, publish, spublish", *mode)
	}

	// listen for C-c
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	w := new(tabwriter.Writer)

	tick := time.NewTicker(time.Duration(*client_update_tick) * time.Second)
	closed, start_time, duration, totalMessages, messageRateTs, rttValues := updateCLI(tick, c, total_messages, w, *test_time, *measureRTT, *mode, rttLatencyChannel)
	messageRate := float64(totalMessages) / float64(duration.Seconds())

	if *cpuprofile != "" {
		pprof.StopCPUProfile()
	}
	fmt.Fprintf(w, "#################################################\n")
	fmt.Fprintf(w, "Mode: %s\n", *mode)
	fmt.Fprintf(w, "Total Duration: %f Seconds\n", duration.Seconds())
	fmt.Fprintf(w, "Message Rate: %f msg/sec\n", messageRate)
	if *measureRTT && (*mode != "publish" && *mode != "spublish") {
		hist := hdrhistogram.New(1, 10_000_000, 3) // 1us to 10s, 3 sig digits
		for _, rtt := range rttValues {
			_ = hist.RecordValue(rtt)
		}
		avg := hist.Mean()
		p50 := hist.ValueAtQuantile(50.0)
		p95 := hist.ValueAtQuantile(95.0)
		p99 := hist.ValueAtQuantile(99.0)
		p999 := hist.ValueAtQuantile(99.9)
		fmt.Fprintf(w, "Avg RTT       %.3f ms\n", avg/1000.0)
		fmt.Fprintf(w, "P50 RTT       %.3f ms\n", float64(p50)/1000.0)
		fmt.Fprintf(w, "P95 RTT       %.3f ms\n", float64(p95)/1000.0)
		fmt.Fprintf(w, "P99 RTT       %.3f ms\n", float64(p99)/1000.0)
		fmt.Fprintf(w, "P999 RTT       %.3f ms\n", float64(p999)/1000.0)
	} else {
	}
	fmt.Fprintf(w, "#################################################\n")
	fmt.Fprint(w, "\r\n")
	w.Flush()

	if strings.Compare(*json_out_file, "") != 0 {

		res := testResult{
			StartTime:             start_time.Unix(),
			Duration:              duration.Seconds(),
			Mode:                  *mode,
			MessageRate:           messageRate,
			TotalMessages:         totalMessages,
			TotalSubscriptions:    total_subscriptions,
			ChannelMin:            *channel_minimum,
			ChannelMax:            *channel_maximum,
			SubscribersPerChannel: *subscribers_per_channel,
			MessagesPerChannel:    *messages_per_channel_subscriber,
			MessageRateTs:         messageRateTs,
			OSSDistributedSlots:   *distributeSubscribers,
			Addresses:             nodesAddresses,
		}
		file, err := json.MarshalIndent(res, "", " ")
		if err != nil {
			log.Fatal(err)
		}

		err = ioutil.WriteFile(*json_out_file, file, 0644)
		if err != nil {
			log.Fatal(err)
		}
	}

	if closed {
		return
	}

	// tell the goroutine to stop
	close(c)
	// and wait for them both to reply back
	wg.Wait()
}

func updateSecondarySlicesCluster(clusterClient *redis.ClusterClient, ctx context.Context) (int, []*redis.Client, []string) {
	var nodeCount = 0
	var nodesAddresses []string
	var nodeClients []*redis.Client
	log.Println("Getting cluster slots info.")
	slots, err := clusterClient.ClusterSlots(ctx).Result()
	if err != nil {
		log.Fatal(err)
	}

	log.Println(fmt.Sprintf("Detailing cluster slots info. Total slot groups: %d", len(slots)))
	for _, slotInfo := range slots {
		log.Println(fmt.Sprintf("\tSlot range start %d end %d. Nodes: %v", slotInfo.Start, slotInfo.End, slotInfo.Nodes))
	}
	log.Println(fmt.Sprintf("Detailing cluster node info"))
	fn := func(ctx context.Context, client *redis.Client) (err error) {
		clusterSlicesMu.Lock()
		nodeClients = append(nodeClients, client)
		addr := client.Conn().String()
		addrS := strings.Split(addr, ":")
		finalAddr := fmt.Sprintf("%s:%s", addrS[0][len(" Redis<")-1:], addrS[1][:len(addrS[1])-3])
		log.Println(fmt.Sprintf("Cluster node pos #%d. Address: %s.", len(nodeClients), finalAddr))
		nodesAddresses = append(nodesAddresses, finalAddr)
		clusterSlicesMu.Unlock()
		return
	}
	clusterClient.ForEachMaster(ctx, fn)
	nodeCount = len(nodesAddresses)
	return nodeCount, nodeClients, nodesAddresses
}
func updateCLI(
	tick *time.Ticker,
	c chan os.Signal,
	message_limit int64,
	w *tabwriter.Writer,
	test_time int,
	measureRTT bool,
	mode string,
	rttLatencyChannel chan int64,
) (bool, time.Time, time.Duration, uint64, []float64, []int64) {

	start := time.Now()
	prevTime := time.Now()
	prevMessageCount := uint64(0)
	prevConnectCount := uint64(0)
	messageRateTs := []float64{}
	tickRttValues := []int64{}
	rttValues := []int64{}

	w.Init(os.Stdout, 25, 0, 1, ' ', tabwriter.AlignRight)

	// Header
	if measureRTT {
		fmt.Fprint(w, "Test Time\tTotal Messages\t Message Rate \tConnect Rate \t")

		if strings.Contains(mode, "subscribe") {
			fmt.Fprint(w, "Active subscriptions\t")
		} else {
			fmt.Fprint(w, "Active publishers\t")
		}
		fmt.Fprint(w, "Avg RTT (ms)\t\n")
	} else {
		fmt.Fprint(w, "Test Time\tTotal Messages\t Message Rate \tConnect Rate \t")
		if strings.Contains(mode, "subscribe") {
			fmt.Fprint(w, "Active subscriptions\t\n")
		} else {
			fmt.Fprint(w, "Active publishers\t\n")
		}
	}
	w.Flush()

	// Main loop
	for {
		select {
		case rtt := <-rttLatencyChannel:
			rttValues = append(rttValues, rtt)
			tickRttValues = append(tickRttValues, rtt)

		case <-tick.C:
			now := time.Now()
			took := now.Sub(prevTime)
			messageRate := float64(totalMessages-prevMessageCount) / float64(took.Seconds())
			connectRate := float64(totalConnects-prevConnectCount) / float64(took.Seconds())

			if prevMessageCount == 0 && totalMessages != 0 {
				start = time.Now()
			}
			if totalMessages != 0 {
				messageRateTs = append(messageRateTs, messageRate)
			}
			prevMessageCount = totalMessages
			prevConnectCount = totalConnects
			prevTime = now

			// Metrics line
			fmt.Fprintf(w, "%.0f\t%d\t%.2f\t%.2f\t", time.Since(start).Seconds(), totalMessages, messageRate, connectRate)

			if strings.Contains(mode, "subscribe") {
				fmt.Fprintf(w, "%d\t", totalSubscribedChannels)
			} else {
				fmt.Fprintf(w, "%d\t", atomic.LoadInt64(&totalPublishers))
			}

			if measureRTT {
				var avgRTTms float64
				if len(tickRttValues) > 0 {
					var total int64
					for _, v := range tickRttValues {
						total += v
					}
					avgRTTms = float64(total) / float64(len(tickRttValues)) / 1000.0
					tickRttValues = tickRttValues[:0]
					fmt.Fprintf(w, "%.3f\t", avgRTTms)
				} else {
					fmt.Fprintf(w, "--\t")
				}
			}

			fmt.Fprint(w, "\r\n")
			w.Flush()

			if message_limit > 0 && totalMessages >= uint64(message_limit) {
				return true, start, time.Since(start), totalMessages, messageRateTs, rttValues
			}
			if test_time > 0 && time.Since(start) >= time.Duration(test_time*int(time.Second)) && totalMessages != 0 {
				return true, start, time.Since(start), totalMessages, messageRateTs, rttValues
			}

		case <-c:
			fmt.Println("received Ctrl-c - shutting down")
			return true, start, time.Since(start), totalMessages, messageRateTs, rttValues
		}
	}
}
