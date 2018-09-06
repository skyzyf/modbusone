package modbusone

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

//SerialContext is an interface implemented by SerialPort, can also be mocked for testing.
type SerialContext interface {
	io.ReadWriteCloser
	//RTUMinDelay returns the minimum required delay between packets for framing
	MinDelay() time.Duration
	//RTUBytesDelay returns the duration it takes to send n bytes
	BytesDelay(n int) time.Duration
	//Stats reporting
	Stats() *Stats
}

type serial struct {
	conn     io.ReadWriteCloser
	baudRate int64
	s        Stats
}

type Stats struct {
	ReadPackets      int64
	CrcErrors        int64
	RemoteErrors     int64
	OtherErrors      int64
	LongReadWarnings int64
	FormateWarnings  int64
	IDDrops          int64
	OtherDrops       int64
}

//Reset the stats to zero
func (s *Stats) Reset() {
	atomic.StoreInt64(&s.ReadPackets, 0)
	atomic.StoreInt64(&s.CrcErrors, 0)
	atomic.StoreInt64(&s.RemoteErrors, 0)
	atomic.StoreInt64(&s.OtherErrors, 0)
	atomic.StoreInt64(&s.LongReadWarnings, 0)
	atomic.StoreInt64(&s.FormateWarnings, 0)
	atomic.StoreInt64(&s.IDDrops, 0)
	atomic.StoreInt64(&s.OtherDrops, 0)
}

//TotalDrops adds up all the errors for the total number of read packets dropped
func (s *Stats) TotalDrops() int64 {
	return atomic.LoadInt64(&s.CrcErrors) + atomic.LoadInt64(&s.RemoteErrors) + atomic.LoadInt64(&s.OtherErrors) +
		atomic.LoadInt64(&s.LongReadWarnings) + atomic.LoadInt64(&s.FormateWarnings) +
		atomic.LoadInt64(&s.IDDrops) + atomic.LoadInt64(&s.OtherDrops)
}

func (s *Stats) String() string {
	return fmt.Sprint(atomic.LoadInt64(&s.CrcErrors), atomic.LoadInt64(&s.RemoteErrors), atomic.LoadInt64(&s.OtherErrors),
		atomic.LoadInt64(&s.LongReadWarnings), atomic.LoadInt64(&s.FormateWarnings),
		atomic.LoadInt64(&s.IDDrops), atomic.LoadInt64(&s.OtherDrops))
}

//NewSerialContext creates a SerialContext from any io.ReadWriteCloser
func NewSerialContext(conn io.ReadWriteCloser, baudRate int64) SerialContext {
	return &serial{conn, baudRate, Stats{}}
}

//Read reads the serial port
func (s *serial) Read(b []byte) (int, error) {
	n, err := s.conn.Read(b)
	return n, err
}

func (s *serial) Write(b []byte) (int, error) {
	debugf("SerialPort Write:%x\n", b)
	n, err := s.conn.Write(b)
	return n, err
}

func (s *serial) Close() error {
	return s.conn.Close()
}

func (s *serial) MinDelay() time.Duration {
	return MinDelay(int64(s.baudRate))
}

func (s *serial) BytesDelay(n int) time.Duration {
	return BytesDelay(int64(s.baudRate), n)
}

func (s *serial) Stats() *Stats {
	return &s.s
}

//MinDelay returns the minimum Delay of 3.5 bytes between packets or 1750 mircos
func MinDelay(baudRate int64) time.Duration {
	delay := 1750 * time.Microsecond
	br := time.Duration(baudRate)
	if br <= 19200 {
		//time it takes to send 3.5 or 7/2 chars (a char of 8 bits takes 11 bits on wire)
		delay = (time.Second*11*7 + (br * 2) - 1) / (br * 2)
	}
	return delay
}

//BytesDelay returns the time it takes to send n bytes in baudRate
func BytesDelay(baudRate int64, n int) time.Duration {
	br := time.Duration(baudRate)
	cs := time.Duration(n)

	return (time.Second*11*cs + br - 1) / br
}
