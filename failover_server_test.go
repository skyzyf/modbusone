package modbusone_test

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiegeo/coloredgoroutine"
	. "github.com/xiegeo/modbusone"
)

type counter struct {
	reads  int64 // first for alignment
	writes int64
	*Stats
}

func (c *counter) total() int64 {
	return c.Stats.TotalDrops() + atomic.LoadInt64(&c.reads) + atomic.LoadInt64(&c.writes)
}

func (c *counter) reset() {
	c.Stats.Reset()
	atomic.StoreInt64(&c.reads, 0)
	atomic.StoreInt64(&c.writes, 0)
}

func (c *counter) same(to *counter) bool {
	return atomic.LoadInt64(&c.reads) == atomic.LoadInt64(&to.reads) &&
		atomic.LoadInt64(&c.writes) == atomic.LoadInt64(&to.writes)
}

func (c *counter) sameInverted(to *counter) bool {
	return atomic.LoadInt64(&c.reads) == atomic.LoadInt64(&to.writes) &&
		atomic.LoadInt64(&c.writes) == atomic.LoadInt64(&to.reads)
}

func (c *counter) String() string {
	return fmt.Sprintf("reads:%v writes:%v drops:%v", atomic.LoadInt64(&c.reads), atomic.LoadInt64(&c.writes), c.TotalDrops())
}

func newTestHandler(name string, t *testing.T) ([]uint16, *SimpleHandler, *counter) {
	var holdingRegisters [100]uint16
	count := counter{}
	shA := &SimpleHandler{
		ReadHoldingRegisters: func(address, quantity uint16) ([]uint16, error) {
			t.Logf("Read %s %v, quantity %v\n", name, address, quantity)
			atomic.AddInt64(&count.reads, int64(quantity))
			return holdingRegisters[address : address+quantity], nil
		},
		WriteHoldingRegisters: func(address uint16, values []uint16) error {
			t.Logf("Write %s %v, quantity %v\n", name, address, len(values))
			atomic.AddInt64(&count.writes, int64(len(values)))
			for i, v := range values {
				holdingRegisters[address+uint16(i)] = v
			}
			return nil
		},
	}
	return holdingRegisters[:], shA, &count
}

func setDelays(f *FailoverSerialConn) {
	f.SecondaryDelay = serverProcessingTime / 2
	f.MissDelay = serverProcessingTime
}

func connectToMockServers(t *testing.T, slaveID byte) (*RTUClient, *FailoverSerialConn, *counter, *counter, *counter, func()) {
	// pipes
	ra, wa := io.Pipe() // server a
	rb, wb := io.Pipe() // server b
	rc, wc := io.Pipe() // client

	// everyone writes to everyone else
	wfa := io.MultiWriter(wb, wc) // write from a, etc...
	wfb := io.MultiWriter(wa, wc)
	wfc := io.MultiWriter(wa, wb)

	sa := NewFailoverConn(newMockSerial("sa", ra, wfa, ra), false, false) // server a connection
	sb := NewFailoverConn(newMockSerial("sb", rb, wfb, rb), true, false)  // server b connection
	cc := newMockSerial("cc", rc, wfc, rc)                                // client connection

	serverA := NewRTUServer(sa, slaveID)
	serverB := NewRTUServer(sb, slaveID)
	client := NewRTUClient(cc, slaveID)

	// faster timeouts during testing
	client.SetServerProcessingTime(serverProcessingTime)
	setDelays(sa)
	setDelays(sb)

	_, shA, countA := newTestHandler("server A", t)
	countA.Stats = sa.Stats()
	_, shB, countB := newTestHandler("server B", t)
	countB.Stats = sb.Stats()
	holdingRegistersC, shC, countC := newTestHandler("client", t)
	countC.Stats = cc.Stats()
	for i := range holdingRegistersC {
		holdingRegistersC[i] = uint16(i + 1<<8)
	}

	go serverA.Serve(shA)
	go serverB.Serve(shB)
	go client.Serve(shC)

	return client, sa, countA, countB, countC, func() {
		serverA.Close()
		serverB.Close()
		client.Close()
	}
}

func TestFailoverServer(t *testing.T) {
	id := byte(0x77)
	client, pc, countA, countB, countC, closeServers := connectToMockServers(t, id)
	defer closeServers()
	exCount := counter{Stats: &Stats{}}
	resetCounts := func() {
		exCount.reset()
		countA.reset()
		countB.reset()
		countC.reset()
	}

	type tc struct {
		fc   FunctionCode
		size uint16
	}
	testCases := []tc{
		{FcWriteSingleRegister, 5},
		{FcWriteMultipleRegisters, 5},
		{FcReadHoldingRegisters, 5},
	}

	_ = os.Stdout
	_ = coloredgoroutine.Colors
	SetDebugOut(coloredgoroutine.Colors(os.Stdout))
	defer func() { SetDebugOut(nil) }()

	t.Run("cold start", func(t *testing.T) {
		reqs, err := MakePDURequestHeadersSized(FcReadHoldingRegisters, 0, 1, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = DoTransactions(client, id, reqs)
		if err == nil {
			t.Fatal("cold start, not expecting any active servers")
		}
		for i := 0; i < 3; /*ServerMissesMax*/ i++ {
			// activates server
			DoTransactions(client, id, reqs)
		}
		time.Sleep(serverProcessingTime * 2)
		if !pc.IsActive() {
			t.Fatal("primary servers should be active")
		}
	})

	for i, ts := range testCases {
		t.Run(fmt.Sprintf("normal %v fc:%v size:%v", i, ts.fc, ts.size), func(t *testing.T) {
			resetCounts()
			reqs, err := MakePDURequestHeadersSized(ts.fc, 0, ts.size, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DoTransactions(client, id, reqs)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(serverProcessingTime * 2)
			if ts.fc.IsWriteToServer() {
				exCount.writes += int64(ts.size)
			} else {
				exCount.reads += int64(ts.size)
			}
			if !exCount.sameInverted(countC) {
				t.Error("client counter     ", countC)
				t.Error("expected (inverted)", exCount)
			}
			if !exCount.same(countA) {
				t.Error("server a counter", countA)
				t.Error("expected        ", exCount)
			}
			if !exCount.same(countB) {
				t.Error("server b counter", countB)
				t.Error("expected        ", exCount)
			}
		})
	}
}
