package modbusone

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

//RTUServer implements Server/Slave side logic for RTU over a SerialContext to
//be used by a ProtocolHandler
type RTUServer struct {
	com          SerialContext
	packetReader PacketReader
	SlaveID      byte
}

//NewRTUServer creates a RTU server on SerialContext listening on slaveID
func NewRTUServer(com SerialContext, slaveID byte) *RTUServer {
	pr, ok := com.(PacketReader)
	if !ok {
		pr = NewRTUPacketReader(com, false)
	}
	r := RTUServer{
		com:          com,
		packetReader: pr,
		SlaveID:      slaveID,
	}
	return &r
}

//Serve runs the server and only returns after unrecoverable error, such as
//SerialContext is closed. Read is assumed to only read full packets,
//as per RTU delay based spec.
func (s *RTUServer) Serve(handler ProtocolHandler) error {
	delay := s.com.MinDelay()

	var rb []byte
	if OverSizeSupport {
		rb = make([]byte, OverSizeMaxRTU)
	} else {
		rb = make([]byte, MaxRTUSize)
	}

	var p PDU

	var ioErr error //make continue do io error checking
	wp := func(pdu PDU, slaveId byte) {
		if slaveId == 0 {
			return
		}
		time.Sleep(delay)
		_, ioErr = s.com.Write(MakeRTU(slaveId, pdu))
	}
	wec := func(err error, slaveId byte) {
		wp(ExceptionReplyPacket(p, ToExceptionCode(err)), slaveId)
	}

	for ioErr == nil {
		var n int
		debugf("RTUServer wait for read\n")
		n, ioErr = s.packetReader.Read(rb)
		if ioErr != nil {
			return ioErr
		}
		r := RTU(rb[:n])
		debugf("RTUServer read packet:%v\n", hex.EncodeToString(r))
		var err error
		p, err = r.GetPDU()
		if err != nil {
			if err == ErrorCrc {
				s.com.Stats().CrcErrors++
			} else {
				s.com.Stats().OtherErrors++
			}
			debugf("RTUServer drop read packet:%v\n", err)
			continue
		}
		if r[0] != 0 && r[0] != s.SlaveID {
			s.com.Stats().IDDrops++
			debugf("RTUServer drop packet to other id:%v\n", r[0])
			continue
		}
		err = p.ValidateRequest()
		if err != nil {
			s.com.Stats().OtherErrors++
			debugf("RTUServer auto return for error:%v\n", err)
			wec(err, r[0])
			continue
		}
		fc := p.GetFunctionCode()
		if fc.IsReadToServer() {
			data, err := handler.OnRead(p)
			if err != nil {
				s.com.Stats().OtherErrors++
				debugf("RTUServer handler.OnOutput error:%v\n", err)
				wec(err, r[0])
				continue
			}
			wp(p.MakeReadReply(data), r[0])
		} else if fc.IsWriteToServer() {
			data, err := p.GetRequestValues()
			if err != nil {
				s.com.Stats().OtherErrors++
				debugf("RTUServer p.GetRequestValues error:%v\n", err)
				wec(err, r[0])
				continue
			}
			err = handler.OnWrite(p, data)
			if err != nil {
				s.com.Stats().OtherErrors++
				debugf("RTUServer handler.OnInput error:%v\n", err)
				wec(err, r[0])
				continue
			}
			wp(p.MakeWriteReply(), r[0])
		}
	}
	return ioErr
}

//Uint64ToSlaveID is a helper function for reading configuration data to SlaveID.
//See also flag.Uint64 and strconv.ParseUint
func Uint64ToSlaveID(n uint64) (byte, error) {
	if n > 247 {
		return 0, errors.New("slaveID must be less than 248")
	}
	return byte(n), nil
}

//DebugOut sets where to print debug messages.
//
//Deprecated: please use SetDebugOut instead.
var DebugOut io.Writer
var debugLock sync.Mutex

//SetDebugOut to print debug messages, set to nil to turn off debug output.
//TODO refactor to use pkg/sync/atomic/#Value once DebugOut is removed
func SetDebugOut(w io.Writer) {
	debugLock.Lock()
	defer debugLock.Unlock()
	DebugOut = w
}

func debugf(format string, a ...interface{}) {
	debugLock.Lock()
	defer debugLock.Unlock()
	if DebugOut == nil {
		return
	}
	fmt.Fprintf(DebugOut, "[%s]", time.Now().Format("06-01-02 15:04:05.000000"))
	fmt.Fprintf(DebugOut, format, a...)
	lf := len(format)
	if lf > 0 && format[lf-1] != '\n' {
		fmt.Fprintln(DebugOut)
	}
}