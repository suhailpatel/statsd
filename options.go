package statsd

import (
	"bytes"
	"strings"
	"time"
)

type config struct {
	Conn   connConfig
	Client clientConfig
}

type clientConfig struct {
	Muted  bool
	Rate   float32
	Prefix string
	Tags   []tag
}

type connConfig struct {
	Addr          string
	ErrorHandler  func(error)
	FlushPeriod   time.Duration
	MaxPacketSize int
	Network       string
	TagFormat     TagFormat
}

// An Option represents an option for a Client. It must be used as an
// argument to New() or Client.Clone().
type Option func(*config)

// Address sets the address of the StatsD daemon.
//
// By default, ":8125" is used. This option is ignored in Client.Clone().
func Address(addr string) Option {
	return Option(func(c *config) {
		c.Conn.Addr = addr
	})
}

// ErrorHandler sets the function called when an error happens when sending
// metrics (e.g. the StatsD daemon is not listening anymore).
//
// By default, these errors are ignored.  This option is ignored in
// Client.Clone().
func ErrorHandler(h func(error)) Option {
	return Option(func(c *config) {
		c.Conn.ErrorHandler = h
	})
}

// FlushPeriod sets how often the Client's buffer is flushed. If p is 0, the
// goroutine that periodically flush the buffer is not lauched and the buffer
// is only flushed when it is full.
//
// By default, the flush period is 100 ms.  This option is ignored in
// Client.Clone().
func FlushPeriod(p time.Duration) Option {
	return Option(func(c *config) {
		c.Conn.FlushPeriod = p
	})
}

// MaxPacketSize sets the maximum packet size in bytes sent by the Client.
//
// By default, it is 1440 to avoid IP fragmentation. This option is ignored in
// Client.Clone().
func MaxPacketSize(n int) Option {
	return Option(func(c *config) {
		c.Conn.MaxPacketSize = n
	})
}

// Network sets the network (udp, tcp, etc) used by the client. See the
// net.Dial documentation (https://golang.org/pkg/net/#Dial) for the available
// network options.
//
// By default, network is udp. This option is ignored in Client.Clone().
func Network(network string) Option {
	return Option(func(c *config) {
		c.Conn.Network = network
	})
}

// Mute sets whether the Client is muted. All methods of a muted Client do
// nothing and return immedialtly.
//
// This option can be used in Client.Clone() only if the parent Client is not
// muted. The clones of a muted Client are always muted.
func Mute(b bool) Option {
	return Option(func(c *config) {
		c.Client.Muted = b
	})
}

// SampleRate sets the sample rate of the Client. It allows sending the metrics
// less often which can be useful for performance intensive code paths.
func SampleRate(rate float32) Option {
	return Option(func(c *config) {
		c.Client.Rate = rate
	})
}

// Prefix appends the prefix that will be used in every bucket name.
//
// Note that when used in cloned, the prefix of the parent Client is not
// replaced but is prepended to the given prefix.
func Prefix(p string) Option {
	return Option(func(c *config) {
		c.Client.Prefix += strings.TrimSuffix(p, ".") + "."
	})
}

// TagFormat represents the format of tags sent by a Client.
type TagFormat uint8

// TagsFormat sets the format of tags.
func TagsFormat(tf TagFormat) Option {
	return Option(func(c *config) {
		c.Conn.TagFormat = tf
	})
}

// Tags appends the given tags to the tags sent with every metrics. If a tag
// already exists, it is replaced.
//
// The tags must be set as key-value pairs. If the number of tags is not even,
// Tags panics.
//
// If the format of tags have not been set using the TagsFormat option, the tags
// will be ignored.
func Tags(tags ...string) Option {
	if len(tags)%2 != 0 {
		panic("statsd: Tags only accepts an even number of arguments")
	}

	return Option(func(c *config) {
		if len(tags) == 0 {
			return
		}

		c.Client.Tags = mergeTags([]tag{}, tags)
	})
}

type tag struct {
	K, V string
}

func joinTags(tf TagFormat, tags []tag) string {
	if len(tags) == 0 || tf == 0 {
		return ""
	}
	join := joinFuncs[tf]
	return join(tags)
}

func splitTags(tf TagFormat, tags string) []tag {
	if len(tags) == 0 || tf == 0 {
		return nil
	}
	split := splitFuncs[tf]
	return split(tags)
}

func mergeTags(tags []tag, newTags []string) []tag {
	if len(newTags)%2 != 0 {
		panic("statsd: Tags only accepts an even number of arguments")
	}

	if len(newTags) == 0 {
		return tags
	}

	output := make([]tag, len(tags))
	for i, t := range tags {
		output[i] = tag{K: t.K, V: t.V}
	}

	for i := 0; i < len(newTags)/2; i++ {
		key := newTags[2*i]
		val := newTags[2*i+1]

		exists := false
		for j, _ := range output {
			if key == output[j].K {
				exists = true
				output[j].V = val
			}
		}

		if !exists {
			output = append(output, tag{K: key, V: val})
		}
	}

	return output
}

const (
	// InfluxDB tag format.
	// See https://influxdb.com/blog/2015/11/03/getting_started_with_influx_statsd.html
	InfluxDB TagFormat = iota + 1
	// Datadog tag format.
	// See http://docs.datadoghq.com/guides/metrics/#tags
	Datadog
)

var (
	joinFuncs = map[TagFormat]func([]tag) string{
		// InfluxDB tag format: ,tag1=payroll,region=us-west
		// https://influxdb.com/blog/2015/11/03/getting_started_with_influx_statsd.html
		InfluxDB: func(tags []tag) string {
			var buf bytes.Buffer
			for _, tag := range tags {
				_ = buf.WriteByte(',')
				_, _ = buf.WriteString(tag.K)
				_ = buf.WriteByte('=')
				_, _ = buf.WriteString(tag.V)
			}
			return buf.String()
		},
		// Datadog tag format: |#tag1:value1,tag2:value2
		// http://docs.datadoghq.com/guides/dogstatsd/#datagram-format
		Datadog: func(tags []tag) string {
			buf := bytes.NewBufferString("|#")
			first := true
			for _, tag := range tags {
				if first {
					first = false
				} else {
					_ = buf.WriteByte(',')
				}
				_, _ = buf.WriteString(tag.K)
				_ = buf.WriteByte(':')
				_, _ = buf.WriteString(tag.V)
			}
			return buf.String()
		},
	}
	splitFuncs = map[TagFormat]func(string) []tag{
		InfluxDB: func(s string) []tag {
			s = s[1:]
			pairs := strings.Split(s, ",")
			tags := make([]tag, len(pairs))
			for i, pair := range pairs {
				kv := strings.Split(pair, "=")
				tags[i] = tag{K: kv[0], V: kv[1]}
			}
			return tags
		},
		Datadog: func(s string) []tag {
			s = s[2:]
			pairs := strings.Split(s, ",")
			tags := make([]tag, len(pairs))
			for i, pair := range pairs {
				kv := strings.Split(pair, ":")
				tags[i] = tag{K: kv[0], V: kv[1]}
			}
			return tags
		},
	}
)
