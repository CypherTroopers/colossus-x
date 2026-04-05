package p2p

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadMessageRejectsOversizedFrame(t *testing.T) {
	var frame bytes.Buffer
	var sz [4]byte
	binary.BigEndian.PutUint32(sz[:], defaultMaxMessageSize+1)
	frame.Write(sz[:])
	if _, err := readMessage(&frame); err == nil {
		t.Fatal("expected oversized frame to fail")
	}
}
