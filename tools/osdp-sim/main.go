// Command osdp-sim is an OSDP PD (reader) simulator for building and testing mypintusan's CP.
//
// It stands up one or more simulated OSDP readers on a virtual multi-drop bus and serves them over
// TCP, so the CP driver, the credential store, the door state machine, schedules, lockdown and the
// whole UI can be built and regression-tested with ZERO hardware. Real readers then become a
// validation step rather than a prerequisite — which matters when the reader is a three-week
// shipment from Shenzhen.
//
// Simulating the happy path is the easy half and the less valuable one. The point is fault
// injection: you cannot make a real reader reply BUSY, skew its sequence numbers, refuse a Secure
// Channel handshake or go silent mid-session on demand, and every one of those is a failure the CP
// must survive. Each -scenario below is a row of the fault table in MYPINTUSAN_OSDP_PLAN.md §4.1.
//
// Usage:
//
//	go run ./tools/osdp-sim -scenario happy -v
//	go run ./tools/osdp-sim -list
//
// then point a CP at 127.0.0.1:4870.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mysayasan/kopiv2/infra/access/osdp"
)

// A scenario builds the bus and starts whatever scripted misbehaviour it needs. faultAfter is when
// a time-based fault should bite, giving a CP time to reach a healthy online state first — a fault
// injected before the session establishes tests a different, easier path.
type scenario struct {
	desc  string
	build func(cfg config) (*Bus, []func(*Bus))
}

type config struct {
	log       *log.Logger
	verbose   bool
	card      []byte
	bits      int
	cardEvery time.Duration
	// pin, when set, is entered on the keypad immediately BEFORE each card presentation.
	// The order is not a detail: the controller buffers a keypad entry and CONSUMES it when
	// the card arrives (services/controller.go takePIN), so a PIN sent after the card is
	// simply left for the next person — which is the behaviour that stops the man behind you
	// in the queue opening the door on your PIN.
	pin        string
	faultAfter time.Duration
	slowReply  time.Duration
	siteKey    osdp.SCBK
}

var scenarios = map[string]scenario{
	"happy": {
		"one healthy reader at address 1, presenting a card periodically",
		func(c config) (*Bus, []func(*Bus)) {
			b := NewBus(c.log, c.verbose, osdp.NewPD(1))
			return b, []func(*Bus){cardLoop(c, 1)}
		},
	},
	"multidrop": {
		"three healthy readers at addresses 1, 2 and 3 on one bus",
		func(c config) (*Bus, []func(*Bus)) {
			b := NewBus(c.log, c.verbose, osdp.NewPD(1), osdp.NewPD(2), osdp.NewPD(3))
			return b, []func(*Bus){cardLoop(c, 1), cardLoop(c, 2), cardLoop(c, 3)}
		},
	},
	"addr-collision": {
		"two brand-new readers BOTH at the factory address 0 — the out-of-box onboarding case",
		func(c config) (*Bus, []func(*Bus)) {
			return NewBus(c.log, c.verbose, osdp.NewPD(0), osdp.NewPD(0)), nil
		},
	},
	"busy": {
		"reader replies BUSY to the first commands of every burst — must be retried, not declared dead",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.Faults.Busy = 3
			b := NewBus(c.log, c.verbose, pd)
			// Re-arm periodically so the CP meets BUSY repeatedly, not just at startup.
			return b, []func(*Bus){repeat(2*time.Second, func(bus *Bus) {
				bus.With(func(pds []*osdp.PD) { pds[0].Faults.Busy = 3 })
			})}
		},
	},
	"bad-sequence": {
		"reader replies with a skewed sequence number — the §3.1 trap that looks like a wiring fault",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.Faults.SequenceSkew = 1
			return NewBus(c.log, c.verbose, pd), nil
		},
	},
	"bad-crc": {
		"every reply has a corrupted CRC — decoder must reject without panicking, framing must resync",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.Faults.BadCRC = true
			return NewBus(c.log, c.verbose, pd), nil
		},
	},
	"garbage": {
		"reader emits junk bytes salted with stray SOMs — exercises frame resynchronisation",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.Faults.Garbage = true
			return NewBus(c.log, c.verbose, pd), nil
		},
	},
	"silent": {
		"reader goes silent after -fault-after (cut bus / dead PSU) — offline supervision + degraded alert",
		func(c config) (*Bus, []func(*Bus)) {
			b := NewBus(c.log, c.verbose, osdp.NewPD(1))
			return b, []func(*Bus){after(c.faultAfter, "reader going silent", func(bus *Bus) {
				bus.With(func(pds []*osdp.PD) { pds[0].Faults.Silent = true })
			})}
		},
	},
	"one-down": {
		"three readers, the middle one dies after -fault-after — the others must keep working",
		func(c config) (*Bus, []func(*Bus)) {
			b := NewBus(c.log, c.verbose, osdp.NewPD(1), osdp.NewPD(2), osdp.NewPD(3))
			return b, []func(*Bus){
				cardLoop(c, 1), cardLoop(c, 3),
				after(c.faultAfter, "reader 2 going silent", func(bus *Bus) {
					bus.With(func(pds []*osdp.PD) { pds[1].Faults.Silent = true })
				}),
			}
		},
	},
	"refuse-sc": {
		"reader REFUSES the Secure Channel handshake — a RequireSecureChannel door must fail closed",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.Faults.RefuseSecureChannel = true
			// A card loop, because "must fail closed" is a claim about what happens WHEN SOMEBODY
			// BADGES. Without one this scenario can only show that a session was refused, which is
			// the easy half; the half that matters is whether the door then opens anyway.
			return NewBus(c.log, c.verbose, pd), []func(*Bus){cardLoop(c, 1)}
		},
	},
	"no-sc": {
		"reader cannot do Secure Channel at all (PDCAP drops the AES-128 bit), and badges anyway",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.Faults.NoSecureChannel = true
			return NewBus(c.log, c.verbose, pd), []func(*Bus){cardLoop(c, 1)}
		},
	},
	"secure": {
		"healthy reader on the SITE key — the Secure Channel happy path",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.SCBK = c.siteKey
			b := NewBus(c.log, c.verbose, pd)
			return b, []func(*Bus){cardLoop(c, 1)}
		},
	},
	"sc-drop": {
		"reader ESTABLISHES Secure Channel then drops it after -fault-after — the harder fail-closed case",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.SCBK = c.siteKey
			b := NewBus(c.log, c.verbose, pd)
			return b, []func(*Bus){
				cardLoop(c, 1),
				after(c.faultAfter, "dropping an established Secure Channel", func(bus *Bus) {
					bus.With(func(pds []*osdp.PD) { pds[0].Faults.DropSecureChannel = true })
				}),
			}
		},
	},
	"wrong-key": {
		"reader holds a DIFFERENT site key — the handshake must fail closed, not downgrade",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			other := c.siteKey
			other[0] ^= 0xFF
			pd.SCBK = other
			return NewBus(c.log, c.verbose, pd), []func(*Bus){cardLoop(c, 1)}
		},
	},
	"default-scbk": {
		"reader still on the well-known default base key — must be capped at `interior` until rekeyed",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.Faults.DefaultSCBK = true
			return NewBus(c.log, c.verbose, pd), []func(*Bus){cardLoop(c, 1)}
		},
	},
	"tamper": {
		"reader asserts tamper after -fault-after — LSTATR/RSTATR alarm path",
		func(c config) (*Bus, []func(*Bus)) {
			b := NewBus(c.log, c.verbose, osdp.NewPD(1))
			return b, []func(*Bus){after(c.faultAfter, "tamper asserted", func(bus *Bus) {
				bus.With(func(pds []*osdp.PD) { pds[0].Faults.Tamper = true })
			})}
		},
	},
	"slow": {
		"reader replies near the poll timeout — must not starve the other PDs on the bus",
		func(c config) (*Bus, []func(*Bus)) {
			pd := osdp.NewPD(1)
			pd.Faults.ReplyDelay = c.slowReply
			b := NewBus(c.log, c.verbose, pd, osdp.NewPD(2))
			return b, []func(*Bus){cardLoop(c, 2)}
		},
	},
}

func main() {
	addr := flag.String("addr", ":4870", "TCP listen address for the simulated bus")
	scen := flag.String("scenario", "happy", "fault scenario to run (-list to see them all)")
	list := flag.Bool("list", false, "list the scenarios and exit")
	// A card with VALID Wiegand-26 parity (facility 1, number 4096), so the default demonstrates
	// the happy path. The old default, `deadbeef`, fails leading even parity — and a CP treats a
	// parity failure as a hard denial, because a card one bit out may be somebody else's. The
	// first live bench of mypintusan spent a run watching every badge denied before working out
	// that the simulator's own default could never be granted.
	cardHex := flag.String("card", "00880040", "card data presented on the bus, hex (must have valid parity for the bit count)")
	pin := flag.String("pin", "", "PIN digits entered on the keypad just before each card (empty = card only)")
	bits := flag.Int("bits", 26, "card bit count (26 = standard Wiegand, 34 = extended)")
	every := flag.Duration("card-every", 8*time.Second, "how often to present a card; 0 disables")
	faultAfter := flag.Duration("fault-after", 15*time.Second, "delay before a time-based fault bites")
	slow := flag.Duration("slow-reply", 400*time.Millisecond, "reply delay for the `slow` scenario")
	keyHex := flag.String("site-key", "a0a1a2a3a4a5a6a7b0b1b2b3b4b5b6b7",
		"16-byte Secure Channel site key, hex — the CP must be given the same one")
	verbose := flag.Bool("v", false, "log every frame in both directions")
	flag.Parse()

	lg := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)

	if *list {
		names := make([]string, 0, len(scenarios))
		for n := range scenarios {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Println("scenarios (each is a row of MYPINTUSAN_OSDP_PLAN.md §4.1):")
		for _, n := range names {
			fmt.Printf("  %-15s %s\n", n, scenarios[n].desc)
		}
		return
	}

	s, ok := scenarios[*scen]
	if !ok {
		lg.Fatalf("unknown scenario %q — run with -list", *scen)
	}
	card, err := hex.DecodeString(strings.TrimPrefix(*cardHex, "0x"))
	if err != nil {
		lg.Fatalf("bad -card %q: %v", *cardHex, err)
	}

	keyBytes, err := hex.DecodeString(strings.TrimPrefix(*keyHex, "0x"))
	if err != nil || len(keyBytes) != 16 {
		lg.Fatalf("bad -site-key %q: need 16 bytes of hex (%v)", *keyHex, err)
	}
	var siteKey osdp.SCBK
	copy(siteKey[:], keyBytes)

	bus, scripts := s.build(config{
		log: lg, verbose: *verbose, card: card, bits: *bits,
		cardEvery: *every, pin: *pin, faultAfter: *faultAfter, slowReply: *slow, siteKey: siteKey,
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		lg.Fatalf("listen %s: %v", *addr, err)
	}
	defer ln.Close()

	lg.Printf("osdp-sim: scenario %q — %s", *scen, s.desc)
	bus.With(func(pds []*osdp.PD) {
		addrs := make([]string, len(pds))
		for i, pd := range pds {
			addrs[i] = fmt.Sprintf("%d", pd.Address)
		}
		lg.Printf("osdp-sim: %d PD(s) at address(es) %s, listening on %s", len(pds), strings.Join(addrs, ","), *addr)
	})

	for _, script := range scripts {
		go script(bus)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				lg.Printf("CP connected from %s", conn.RemoteAddr())
				if err := bus.Serve(conn); err != nil {
					lg.Printf("CP %s disconnected: %v", conn.RemoteAddr(), err)
				} else {
					lg.Printf("CP %s disconnected", conn.RemoteAddr())
				}
			}()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	lg.Print("osdp-sim: shutting down")
}

// cardLoop presents a card at the given PD address on a fixed cadence.
func cardLoop(c config, addr uint8) func(*Bus) {
	return func(b *Bus) {
		if c.cardEvery <= 0 {
			return
		}
		for range time.Tick(c.cardEvery) {
			b.With(func(pds []*osdp.PD) {
				for _, pd := range pds {
					if pd.Address != addr {
						continue
					}
					// KEYPAD FIRST, then the card. The CP buffers the digits and consumes
					// them when the card arrives, so the reverse order presents a card with
					// no PIN and leaves the digits waiting for whoever badges next.
					if c.pin != "" {
						pd.PresentKeypad(0, []byte(c.pin))
						c.log.Printf("PIN entered at PD %d: %s", addr, strings.Repeat("*", len(c.pin)))
					}
					pd.PresentCard(osdp.CardRead{Format: 1, BitCount: uint16(c.bits), Data: c.card})
					c.log.Printf("card presented at PD %d: % x (%d bits)", addr, c.card, c.bits)
				}
			})
		}
	}
}

// after fires a one-shot scripted fault, announcing it so the operator watching the log can
// correlate the CP's reaction with the injection.
func after(d time.Duration, what string, fn func(*Bus)) func(*Bus) {
	return func(b *Bus) {
		time.Sleep(d)
		if b.log != nil {
			b.log.Printf("!! injecting fault: %s", what)
		}
		fn(b)
	}
}

// repeat re-applies a fault on a cadence, for faults a PD consumes (like a BUSY budget).
func repeat(d time.Duration, fn func(*Bus)) func(*Bus) {
	return func(b *Bus) {
		for range time.Tick(d) {
			fn(b)
		}
	}
}
