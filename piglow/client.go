package piglow

type Client struct {
	glow   *PiGlow
	levels []uint8
}

// Client returns a new client instance. Each client has its own virtual
// set of LEDs - the levels from all current clients will be added to
// produce the final result.
//
// This is useful when several independent goroutines wish to use the
// same PiGlow.
//
// The client must be closed after use.
func (p *PiGlow) Client() *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	client := &Client{
		glow:   p,
		levels: make([]uint8, NumLEDs),
	}
	p.clients = append(p.clients, client)
	return client
}

// SetBrightness sets the brightness of all the LEDs in the given
// set to the given level.
func (c *Client) SetBrightness(leds Set, level uint8) error {
	if leds == 0 {
		return nil
	}
	buf := make([]byte, 2)
	glow := c.glow
	glow.mu.Lock()
	defer glow.mu.Unlock()
	for i := LED(0); i < NumLEDs; i++ {
		if leds&(1<<uint(i)) == 0 {
			continue
		}
		c.levels[i] = level
		total := 0
		for _, c := range glow.clients {
			total += int(c.levels[i])
		}
		if total > 255 {
			total = 255
		}
		buf[0] = byte(i + 1)
		buf[1] = gamma[total]
		glow.conn.Write(buf)
	}
	glow.conn.Write(update)
	return nil
}

// Close closes the given client. All its LEDs will be reset.
func (c *Client) Close() error {
	c.SetBrightness(allSet, 0)
	glow := c.glow
	glow.mu.Lock()
	defer glow.mu.Unlock()
	for i, glowc := range c.glow.clients {
		if glowc == c {
			e := len(c.glow.clients) - 1
			glow.clients[i], glow.clients[e] = glow.clients[e], glow.clients[i]
			glow.clients = glow.clients[:e]
			break
		}
	}
	return nil
}
