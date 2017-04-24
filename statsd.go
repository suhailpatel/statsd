package statsd

import "time"

// A Client represents a StatsD client.
type Client struct {
	conn      *conn
	muted     bool
	rate      float32
	prefix    string
	tagFormat TagFormat
	tags      []tag
}

// New returns a new Client.
func New(opts ...Option) (*Client, error) {
	// The default configuration.
	conf := &config{
		Client: clientConfig{
			Rate: 1,
		},
		Conn: connConfig{
			Addr:        ":8125",
			FlushPeriod: 100 * time.Millisecond,
			// Worst-case scenario:
			// Ethernet MTU - IPv6 Header - TCP Header = 1500 - 40 - 20 = 1440
			MaxPacketSize: 1440,
			Network:       "udp",
		},
	}
	for _, o := range opts {
		o(conf)
	}

	conn, err := newConn(conf.Conn, conf.Client.Muted)
	c := &Client{
		conn:  conn,
		muted: conf.Client.Muted,
	}
	if err != nil {
		c.muted = true
		return c, err
	}
	c.rate = conf.Client.Rate
	c.prefix = conf.Client.Prefix
	c.tagFormat = conf.Client.TagFormat
	c.tags = conf.Client.Tags
	return c, nil
}

// Clone returns a clone of the Client. The cloned Client inherits its
// configuration from its parent.
//
// All cloned Clients share the same connection, so cloning a Client is a cheap
// operation.
func (c *Client) Clone(opts ...Option) *Client {
	tf := c.conn.tagFormat
	conf := &config{
		Client: clientConfig{
			Rate:   c.rate,
			Prefix: c.prefix,
			Tags:   c.tags,
		},
	}
	for _, o := range opts {
		o(conf)
	}

	clone := &Client{
		conn:      c.conn,
		muted:     c.muted || conf.Client.Muted,
		rate:      conf.Client.Rate,
		prefix:    conf.Client.Prefix,
		tagformat: tf,
		tags:      conf.Client.Tags,
	}
	clone.conn = c.conn
	return clone
}

// Count adds n to bucket. Tags should be specified as K, V pairs
func (c *Client) Count(bucket string, n interface{}, tags ...string) {
	if c.skip() {
		return
	}
	tagstr := mergeTags(c.tagFormat, c.tags, tags)
	c.conn.metric(c.prefix, bucket, n, "c", c.rate, tagstr)
}

func (c *Client) skip() bool {
	return c.muted || (c.rate != 1 && randFloat() > c.rate)
}

// Increment increment the given bucket. It is equivalent to Count(bucket, 1).
func (c *Client) Increment(bucket string) {
	c.Count(bucket, 1)
}

// Gauge records an absolute value for the given bucket.
func (c *Client) Gauge(bucket string, value interface{}, tags ...string) {
	if c.skip() {
		return
	}
	tagstr := mergeTags(c.tagFormat, c.tags, tags)
	c.conn.gauge(c.prefix, bucket, value, tagstr)
}

// Timing sends a timing value to a bucket.
func (c *Client) Timing(bucket string, value interface{}) {
	if c.skip() {
		return
	}
	tagstr := mergeTags(c.tagFormat, c.tags, tags)
	c.conn.metric(c.prefix, bucket, value, "ms", c.rate, tagstr)
}

// Histogram sends an histogram value to a bucket.
func (c *Client) Histogram(bucket string, value interface{}, tags ...string) {
	if c.skip() {
		return
	}
	tagstr := mergeTags(c.tagFormat, c.tags, tags)
	c.conn.metric(c.prefix, bucket, value, "h", c.rate, tagstr)
}

// A Timing is an helper object that eases sending timing values.
type Timing struct {
	start time.Time
	c     *Client
}

// NewTiming creates a new Timing.
func (c *Client) NewTiming() Timing {
	return Timing{start: now(), c: c}
}

// Send sends the time elapsed since the creation of the Timing.
func (t Timing) Send(bucket string) {
	t.c.Timing(bucket, int(t.Duration()/time.Millisecond))
}

// Duration returns the time elapsed since the creation of the Timing.
func (t Timing) Duration() time.Duration {
	return now().Sub(t.start)
}

// Unique sends the given value to a set bucket.
func (c *Client) Unique(bucket string, value string, tags ...string) {
	if c.skip() {
		return
	}
	tagstr := mergeTags(c.tagFormat, c.tags, tags)
	c.conn.unique(c.prefix, bucket, value, tagstr)
}

// Flush flushes the Client's buffer.
func (c *Client) Flush() {
	if c.muted {
		return
	}
	c.conn.mu.Lock()
	c.conn.flush(0)
	c.conn.mu.Unlock()
}

// Close flushes the Client's buffer and releases the associated ressources. The
// Client and all the cloned Clients must not be used afterward.
func (c *Client) Close() {
	if c.muted {
		return
	}
	c.conn.mu.Lock()
	c.conn.flush(0)
	c.conn.handleError(c.conn.w.Close())
	c.conn.closed = true
	c.conn.mu.Unlock()
}
