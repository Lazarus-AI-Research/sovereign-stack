package dockerapi

import (
	"encoding/binary"
	"testing"
)

func frame(stream byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

func TestDemuxLogs(t *testing.T) {
	raw := append(frame(1, "out line\n"), frame(2, "err line\n")...)
	if got := DemuxLogs(raw); got != "out line\nerr line\n" {
		t.Errorf("DemuxLogs = %q", got)
	}
}

func TestDemuxLogsTruncatedFrame(t *testing.T) {
	raw := frame(1, "complete\n")
	raw = append(raw, []byte{1, 0, 0, 0, 0, 0, 0, 99, 'x'}...) // header claims 99 bytes
	if got := DemuxLogs(raw); got != "complete\n" {
		t.Errorf("DemuxLogs with truncated frame = %q", got)
	}
}
