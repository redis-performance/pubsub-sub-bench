package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const (
	redisPoolSize              = "pool_size"
	redisPoolSizeDefault       = 0
	redisTLSCA                 = "tls_ca"
	redisTLSCert               = "tls_cert"
	redisTLSKey                = "tls_key"
	redisTLSInsecureSkipVerify = "tls_insecure_skip_verify"
)

var totalMessages uint64
var totalSubscribedChannels int64
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

func subscriberRoutine(clientName, mode string, channels []string, printMessages bool, connectionReconnectInterval int, ctx context.Context, wg *sync.WaitGroup, client *redis.Client) {
	// Tell the caller we've stopped
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
			if printMessages {
				fmt.Println(fmt.Sprintf("received message in channel %s. Message: %s", msg.Channel, msg.Payload))
			}
			atomic.AddUint64(&totalMessages, 1)
		}
	}
}

func main() {
	host := flag.String("host", "127.0.0.1", "redis host.")
	port := flag.String("port", "6379", "redis port.")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to file")
	password := flag.String("a", "", "Password for Redis Auth.")
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
	totalCreatedClients := 0
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
				new_channel_id := rand.Intn(*channel_maximum) + *channel_minimum
				new_channel := fmt.Sprintf("%s%d", *subscribe_prefix, new_channel_id)
				channels = append(channels, new_channel)
			}
			totalCreatedClients++
			subscriberName := fmt.Sprintf("subscriber#%d", client_id)
			var client *redis.Client
			var err error = nil
			ctx = context.Background()
			// In case of SSUBSCRIBE the node is associated the to the channel name
			if strings.Compare(*mode, "ssubscribe") == 0 && *distributeSubscribers == true {
				firstChannel := channels[0]
				client, err = clusterClient.MasterForKey(ctx, firstChannel)
				if err != nil {
					log.Fatal(err)
				}
				if *verbose {
					log.Println(fmt.Sprintf("client %d is a CLUSTER client connected to %v. Subscriber name %s", totalCreatedClients, client.String(), subscriberName))
				}
			} else {
				nodes_pos := client_id % nodeCount
				addr := nodesAddresses[nodes_pos]
				client = nodeClients[nodes_pos]
				if *verbose {
					log.Println(fmt.Sprintf("client %d is a STANDALONE client connected to node %d (address %s). Subscriber name %s", totalCreatedClients, nodes_pos, addr, subscriberName))
				}
				err = client.Ping(ctx).Err()
				if err != nil {
					log.Fatal(err)
				}
			}
			wg.Add(1)
			connectionReconnectInterval := 0
			if *max_reconnect_interval == *min_reconnect_interval {
				connectionReconnectInterval = *max_reconnect_interval
			} else {
				connectionReconnectInterval = rand.Intn(*max_reconnect_interval-*min_reconnect_interval) + *min_reconnect_interval
			}
			if connectionReconnectInterval > 0 {
				log.Println(fmt.Sprintf("Using reconnection interval of %d milliseconds for subscriber: %s", connectionReconnectInterval, subscriberName))
			}
			log.Println(fmt.Sprintf("subscriber: %s. Total channels %d: %v", subscriberName, len(channels), channels))
			go subscriberRoutine(subscriberName, *mode, channels, *printMessages, connectionReconnectInterval, ctx, &wg, client)
			// }
		}
	}

	// listen for C-c
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	w := new(tabwriter.Writer)

	tick := time.NewTicker(time.Duration(*client_update_tick) * time.Second)
	closed, start_time, duration, totalMessages, messageRateTs := updateCLI(tick, c, total_messages, w, *test_time)
	messageRate := float64(totalMessages) / float64(duration.Seconds())

	if *cpuprofile != "" {
		pprof.StopCPUProfile()
	}

	fmt.Fprint(w, fmt.Sprintf("#################################################\nTotal Duration %f Seconds\nMessage Rate %f\n#################################################\n", duration.Seconds(), messageRate))
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

func updateCLI(tick *time.Ticker, c chan os.Signal, message_limit int64, w *tabwriter.Writer, test_time int) (bool, time.Time, time.Duration, uint64, []float64) {

	start := time.Now()
	prevTime := time.Now()
	prevMessageCount := uint64(0)
	prevConnectCount := uint64(0)
	messageRateTs := []float64{}

	w.Init(os.Stdout, 25, 0, 1, ' ', tabwriter.AlignRight)
	fmt.Fprint(w, fmt.Sprintf("Test Time\tTotal Messages\t Message Rate \tConnect Rate \tActive subscriptions\t"))
	fmt.Fprint(w, "\n")
	w.Flush()
	for {
		select {
		case <-tick.C:
			{
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

				fmt.Fprint(w, fmt.Sprintf("%.0f\t%d\t%.2f\t%.2f\t%d\t", time.Since(start).Seconds(), totalMessages, messageRate, connectRate, totalSubscribedChannels))
				fmt.Fprint(w, "\r\n")
				w.Flush()
				if message_limit > 0 && totalMessages >= uint64(message_limit) {
					return true, start, time.Since(start), totalMessages, messageRateTs
				}
				if test_time > 0 && time.Since(start) >= time.Duration(test_time*1000*1000*1000) && totalMessages != 0 {
					return true, start, time.Since(start), totalMessages, messageRateTs
				}

				break
			}

		case <-c:
			fmt.Println("received Ctrl-c - shutting down")
			return true, start, time.Since(start), totalMessages, messageRateTs
		}
	}
	return false, start, time.Since(start), totalMessages, messageRateTs
}
