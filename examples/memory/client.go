package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/xiegeo/modbusone"
)

func runClient(c *modbusone.RTUClient) {
	// make these messages stand out with color
	println := color.New(color.FgYellow).PrintlnFunc()
	println(`Send requests by function code, address, and quantity.` +
		` such as "2 0 12" (base 10)`)
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		line = strings.Trim(line, " \r\n")
		if err != nil {
			println(err)
			continue
		}
		ts := strings.Split(line, " ")
		fc, a, q, err := parseRequest(ts)
		if err != nil {
			println(err)
			continue
		}
		var limit uint16
		if fc.IsWriteToServer() {
			limit = fc.MaxPerPacketSized((*writeSizeLimit) - 3)
		} else {
			limit = fc.MaxPerPacketSized((*readSizeLimit) - 3)
		}
		println("limit", limit)
		reqs, err := modbusone.MakePDURequestHeadersSized(fc, a, q, limit, nil)
		if err != nil {
			println(err)
			continue
		}
		println("doing ", fc, a, q, "in", len(reqs), "requests")
		n, err := modbusone.DoTransactions(c, c.SlaveID, reqs)
		if err != nil {
			println(err, "in request", n+1, "/", len(reqs), reqs[n])
			continue
		}
		println("finished", n, "requests")
	}
}

func parseRequest(ts []string) (fc modbusone.FunctionCode, address, quantity uint16, err error) {
	quantity = 1 // default value
	var n uint64

	if len(ts) == 0 {
		err = fmt.Errorf("function code required")
		return
	}
	n, err = strconv.ParseUint(ts[0], 10, 7)
	if err != nil {
		err = fmt.Errorf("function code %v parse error: %v", ts[0], err)
		return
	}
	fc = modbusone.FunctionCode(n)
	if !fc.Valid() {
		err = fmt.Errorf("function code %v is not supported", fc)
		return
	}

	if len(ts) < 2 {
		return
	}
	n, err = strconv.ParseUint(ts[1], 10, 16)
	if err != nil {
		err = fmt.Errorf("address %v parse error: %v", ts[1], err)
		return
	}
	address = uint16(n)

	if len(ts) < 3 {
		return
	}
	n, err = strconv.ParseUint(ts[2], 10, 16)
	if err != nil {
		err = fmt.Errorf("quantity %v parse error: %v", ts[2], err)
		return
	}
	quantity = uint16(n)
	return
}
