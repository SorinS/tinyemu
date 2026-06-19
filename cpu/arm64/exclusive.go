package arm64

// Load/store exclusive group (bits[29:24]=001000): the exclusive primitives
// ldxr/stxr (+ acquire/release ldaxr/stlxr), the pair forms ldxp/stxp, and the
// ordered-but-not-exclusive ldar/stlr. Modelled for a single core: a stxr
// succeeds while the local monitor (set by the matching ldxr) is still armed.
//
// Encoding: size(31:30) 001000 o2(23) L(22) o1(21) Rs(20:16) o0(15) Rt2(14:10)
//           Rn(9:5) Rt(4:0).
func (c *CPU) execLoadStoreExclusive(w uint32) error {
	size := (w >> 30) & 3
	o2 := (w >> 23) & 1
	load := (w>>22)&1 == 1
	pair := (w>>21)&1 == 1
	rs := (w >> 16) & 0x1F
	rt2 := (w >> 10) & 0x1F
	rn := (w >> 5) & 0x1F
	rt := w & 0x1F
	nbytes := 1 << size
	addr := c.readX(rn, true, true) // base is SP-capable

	// o2=1 marks the non-exclusive ordered forms: ldar (load-acquire) / stlr
	// (store-release). Ordering is a no-op on this in-order model.
	if o2 == 1 {
		if load { // ldar
			v, err := c.readMem(addr, nbytes)
			if err != nil {
				return err
			}
			c.writeX(rt, size == 3, false, v)
		} else { // stlr
			if err := c.writeMem(addr, c.readX(rt, size == 3, false), nbytes); err != nil {
				return err
			}
		}
		return nil
	}

	if load { // ldxr / ldaxr / ldxp / ldaxp — arm the monitor
		if pair {
			v1, err := c.readMem(addr, nbytes)
			if err != nil {
				return err
			}
			v2, err := c.readMem(addr+uint64(nbytes), nbytes)
			if err != nil {
				return err
			}
			c.writeX(rt, size == 3, false, v1)
			c.writeX(rt2, size == 3, false, v2)
		} else {
			v, err := c.readMem(addr, nbytes)
			if err != nil {
				return err
			}
			c.writeX(rt, size == 3, false, v)
		}
		c.exclMonitor, c.exclAddr = true, addr
		return nil
	}

	// stxr / stlxr / stxp / stlxp — succeed only while the monitor is armed for
	// this address; Ws (Rs) gets 0 on success, 1 on failure.
	if !c.exclMonitor || c.exclAddr != addr {
		c.writeX(rs, false, false, 1)
		return nil
	}
	if pair {
		if err := c.writeMem(addr, c.readX(rt, size == 3, false), nbytes); err != nil {
			return err
		}
		if err := c.writeMem(addr+uint64(nbytes), c.readX(rt2, size == 3, false), nbytes); err != nil {
			return err
		}
	} else {
		if err := c.writeMem(addr, c.readX(rt, size == 3, false), nbytes); err != nil {
			return err
		}
	}
	c.exclMonitor = false
	c.writeX(rs, false, false, 0)
	return nil
}
