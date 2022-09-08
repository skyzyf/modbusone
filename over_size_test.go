package modbusone_test

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	. "github.com/xiegeo/modbusone"
)

var _ = os.Stdout

func connectToMockServer(slaveID byte) io.ReadWriteCloser {
	r1, w1 := io.Pipe() // pipe from client to server
	r2, w2 := io.Pipe() // pipe from server to client

	cc := newMockSerial("c", r2, w1, w1, w2) // client connection
	sc := newMockSerial("s", r1, w2, w2)     // server connection

	server := NewRTUServer(sc, slaveID)

	sh := &SimpleHandler{
		WriteHoldingRegisters: func(address uint16, values []uint16) error {
			return nil
		},
		ReadHoldingRegisters: func(address, quantity uint16) ([]uint16, error) {
			return make([]uint16, quantity), nil
		},
	}
	go server.Serve(sh)
	return cc
}

func TestOverSize(t *testing.T) {
	// DebugOut = os.Stdout
	slaveID := byte(0x11)
	cct := connectToMockServer(slaveID)
	defer cct.Close()
	pdu := PDU(
		append([]byte{
			byte(FcWriteMultipleRegisters),
			0, 0, 0, 200, 0,
		}, make([]byte, 400)...))
	rtu := MakeRTU(slaveID, pdu)
	cct.Write([]byte(rtu))

	bchan := make(chan []byte)
	go func() {
		b := make([]byte, 1000)
		n, err := cct.Read(b)
		if n == 0 && err != nil {
			return
		}
		bchan <- b[:n]
	}()
	timeout := time.NewTimer(time.Second / 20)
	select {
	case b := <-bchan:
		t.Fatalf("should not complete read %x", b)
	case <-timeout.C:
	}

	OverSizeSupport = true
	OverSizeMaxRTU = 512
	defer func() {
		OverSizeSupport = false
	}()

	// New server with OverSizeSupport
	cc := connectToMockServer(slaveID)
	defer cc.Close()
	cc.Write([]byte(rtu))
	go func() {
		for {
			b := make([]byte, 1000)
			n, err := cc.Read(b)
			if n == 0 && err != nil {
				return
			}
			bchan <- b[:n]
		}
	}()
	timeout.Reset(time.Second)
	select {
	case b := <-bchan:
		if fmt.Sprintf("%x", b) != "1110000000c8c30f" {
			t.Fatalf("got unexpected read %x", b)
		}
	case <-timeout.C:
		t.Fatalf("should not time out")
	}

	pdu = PDU([]byte{
		byte(FcReadHoldingRegisters),
		0, 0, 0, 200,
	})
	rtu = MakeRTU(slaveID, pdu)
	cc.Write([]byte(rtu))

	select {
	case b := <-bchan:
		// 0x90 is from 200 * 2 = 0x0190
		if fmt.Sprintf("%x", b[:5]) != "1103900000" {
			t.Fatalf("got unexpected read %x", b)
		}
	case <-timeout.C:
		t.Fatalf("should not time out")
	}
}
