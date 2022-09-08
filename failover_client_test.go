package modbusone_test

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/xiegeo/coloredgoroutine"
	. "github.com/xiegeo/modbusone"
)

var serverProcessingTime = time.Second / 5

func connectMockClients(t *testing.T, slaveID byte) (*FailoverRTUClient, *FailoverRTUClient, *FailoverSerialConn, *counter, *counter, *counter, func()) {
	// pipes
	ra, wa := io.Pipe() // client a
	rb, wb := io.Pipe() // client b
	rc, wc := io.Pipe() // server

	// everyone writes to everyone else
	wfa := io.MultiWriter(wb, wc) // write from a, etc...
	wfb := io.MultiWriter(wa, wc)
	wfc := io.MultiWriter(wa, wb)

	ca := NewFailoverConn(newMockSerial("ca", ra, wfa, ra), false, true) // client a connection
	cb := NewFailoverConn(newMockSerial("cb", rb, wfb, rb), true, true)  // client b connection
	sc := newMockSerial("sc", rc, wfc, rc)                               // server connection

	clientA := NewFailoverRTUClient(ca, false, slaveID)
	clientB := NewFailoverRTUClient(cb, true, slaveID)
	server := NewRTUServer(sc, slaveID)

	// faster timeouts during testing
	clientA.SetServerProcessingTime(serverProcessingTime)
	clientB.SetServerProcessingTime(serverProcessingTime)
	setDelays(ca)
	setDelays(cb)

	_, shA, countA := newTestHandler("client A", t)
	countA.Stats = ca.Stats()
	_, shB, countB := newTestHandler("client B", t)
	countB.Stats = cb.Stats()
	holdingRegistersC, shC, countC := newTestHandler("server", t)
	countC.Stats = sc.Stats()
	for i := range holdingRegistersC {
		holdingRegistersC[i] = uint16(i + 1<<8)
	}

	go clientA.Serve(shA)
	go clientB.Serve(shB)
	go server.Serve(shC)

	return clientA, clientB, ca, countA, countB, countC, func() {
		clientA.Close()
		clientB.Close()
		server.Close()
	}
}

var testFailoverClientCount = 0

func TestFailoverClient(t *testing.T) {
	// t.Skip()

	id := byte(0x77)
	clientA, clientB, pc, countA, countB, countC, closeClients := connectMockClients(t, id)
	defer closeClients()
	exCount := &counter{Stats: &Stats{}}
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
	testFailoverClientCount++
	// fmt.Fprintf(os.Stdout, "=== TestFailoverClient (%v) logging started goroutines (%v) ===\n", testFailoverClientCount, runtime.NumGoroutine())
	defer func() {
		SetDebugOut(nil)
	}()

	t.Run("cold start", func(t *testing.T) {
		reqs, err := MakePDURequestHeadersSized(FcWriteSingleRegister, 0, 1, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; /*MissesMax*/ i++ {
			// activates client
			go DoTransactions(clientA, id, reqs)
			DoTransactions(clientB, id, reqs)
		}
		time.Sleep(serverProcessingTime)
		if !pc.IsActive() {
			t.Fatal("primary client should be active")
		}
	})

	for i, ts := range testCases {
		t.Run(fmt.Sprintf("normal %v fc:%v size:%v", i, ts.fc, ts.size), func(t *testing.T) {
			resetCounts()
			if ts.fc.IsReadToServer() {
				exCount.writes += int64(ts.size)
			} else {
				exCount.reads += int64(ts.size)
			}
			reqs, err := MakePDURequestHeadersSized(ts.fc, 0, ts.size, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			go DoTransactions(clientA, id, reqs)
			DoTransactions(clientB, id, reqs)

			time.Sleep(serverProcessingTime)

			if !exCount.sameInverted(countC) {
				t.Error("server counter     ", countC)
				t.Error("expected (inverted)", exCount)
				t.Error(countC.Stats)
			}
			if !exCount.same(countA) {
				t.Error("client a counter", countA)
				t.Error("expected        ", exCount)
				t.Error(countA.Stats)
			}
			if !exCount.same(countB) {
				t.Error("client b counter", countB)
				t.Error("expected        ", exCount)
				t.Error(countB.Stats)
			}
		})
	}
}
