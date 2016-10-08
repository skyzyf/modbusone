package crc

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestRTU(t *testing.T) {
	testCases := [][]byte{
		[]byte{0x02, 0x07, 0x41, 0x12},
		//from http://www.simplymodbus.ca/FC01.htm
		[]byte{0x11, 0x01, 0x00, 0x13, 0x00, 0x25, 0x0E, 0x84},
		[]byte{0x11, 0x01, 0x05, 0xCD, 0x6B, 0xB2, 0x0E, 0x1B, 0x45, 0xE6},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%v:%v", i, hex.EncodeToString(tc)), func(t *testing.T) {
			if !Validate(tc) {
				t.Fatal("crc invalid")
			}
			l := len(tc)
			b1 := tc[l-2]
			b2 := tc[l-1]
			tc = Append(tc[:l-2])
			if b1 != tc[l-2] || b2 != tc[l-1] {
				t.Fatalf("crc calucation failed %x %x != %x %x", b1, b2, tc[l-2], tc[l-1])
			}
		})
	}
}