module github.com/RedisLabs/pubsub-sub-bench

go 1.13

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/mediocregopher/radix/v4 v4.1.2
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/redis/go-redis/v9 v9.0.3
	github.com/stretchr/testify v1.8.0 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
)

replace github.com/redis/go-redis/v9 => github.com/filipecosta90/go-redis/v9 v9.0.0-20230429203646-959c94037c1e
