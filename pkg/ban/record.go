package ban

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"time"
)

// Record represents a ban event.
// Addr is the unique key of a Record.
type Record struct {
	Addr      netip.Addr
	CreatedAt time.Time
	ExpiresAt time.Time
	Blame     string
}

func appendKey(b []byte, addr netip.Addr) []byte {
	b, _ = addr.AppendBinary(b)
	return b
}

func addrFromKey(key []byte) (netip.Addr, error) {
	switch len(key) {
	case 4:
		var b4 [4]byte
		copy(b4[:], key)
		return netip.AddrFrom4(b4), nil
	case 16:
		var b16 [16]byte
		copy(b16[:], key)
		return netip.AddrFrom16(b16), nil
	default:
		return netip.Addr{}, fmt.Errorf("addrFromKey: invalid key length: (%d bytes)", len(key))
	}
}

// Format: [CreatedAt:8][ExpiresAt:8][BlameLen:2][Blame:N]
func encodeRecord(buf []byte, r *Record) []byte {
	buf = binary.LittleEndian.AppendUint64(buf, uint64(r.CreatedAt.UnixNano()))
	buf = binary.LittleEndian.AppendUint64(buf, uint64(r.ExpiresAt.UnixNano()))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(r.Blame)))
	buf = append(buf, r.Blame...)
	return buf
}

func decodeRecord(addr netip.Addr, data []byte) (Record, error) {
	if len(data) < 18 {
		return Record{}, fmt.Errorf("decodeRecord: data too short (%d bytes)", len(data))
	}
	blameLen := int(binary.LittleEndian.Uint16(data[16:18]))
	if len(data) != 18+blameLen {
		return Record{}, fmt.Errorf("decodeRecord: data length mismatch (expected %d, got %d)", 18+blameLen, len(data))
	}
	return Record{
		Addr:      addr,
		CreatedAt: time.Unix(0, int64(binary.LittleEndian.Uint64(data[0:8]))).UTC(),
		ExpiresAt: time.Unix(0, int64(binary.LittleEndian.Uint64(data[8:16]))).UTC(),
		Blame:     string(data[18:]),
	}, nil
}

func getRecordExpiresAt(data []byte) (time.Time, error) {
	if len(data) < 18 {
		return time.Time{}, fmt.Errorf("decodeRecord: data too short (%d bytes)", len(data))
	}
	return time.Unix(0, int64(binary.LittleEndian.Uint64(data[8:16]))).UTC(), nil
}
