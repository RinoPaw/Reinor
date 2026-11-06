package wire

import (
	"errors"
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
)

const MaxFrame = 256 * 1024

type Envelope struct {
	Type         string
	DeviceID     string
	TaskID       string
	Command      string
	ExitCode     int32
	Stdout       []byte
	Stderr       []byte
	TimeoutMS    uint32
	OS           string
	Error        string
	Capabilities []byte
}

func str(b []byte, n protowire.Number, s string) []byte {
	if s == "" {
		return b
	}
	b = protowire.AppendTag(b, n, protowire.BytesType)
	return protowire.AppendString(b, s)
}
func bytesField(b []byte, n protowire.Number, v []byte) []byte {
	if len(v) == 0 {
		return b
	}
	b = protowire.AppendTag(b, n, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}
func varint(b []byte, n protowire.Number, v uint64) []byte {
	if v == 0 {
		return b
	}
	b = protowire.AppendTag(b, n, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}
func Marshal(v Envelope) ([]byte, error) {
	b := make([]byte, 0, 256)
	b = str(b, 1, v.Type)
	b = str(b, 2, v.DeviceID)
	b = str(b, 3, v.TaskID)
	b = str(b, 4, v.Command)
	b = varint(b, 5, uint64(v.ExitCode))
	b = bytesField(b, 6, v.Stdout)
	b = bytesField(b, 7, v.Stderr)
	b = varint(b, 8, uint64(v.TimeoutMS))
	b = str(b, 9, v.OS)
	b = str(b, 10, v.Error)
	b = bytesField(b, 20, v.Capabilities)
	if len(b) > MaxFrame {
		return nil, fmt.Errorf("protobuf frame too large: %d", len(b))
	}
	return b, nil
}
func Unmarshal(b []byte) (Envelope, error) {
	var out Envelope
	if len(b) > MaxFrame {
		return out, errors.New("frame too large")
	}
	for len(b) > 0 {
		field, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return out, errors.New("malformed protobuf tag")
		}
		b = b[n:]
		switch field {
		case 1, 2, 3, 4, 6, 7, 9, 10, 20:
			if typ != protowire.BytesType {
				return out, fmt.Errorf("bad wire type for field %d", field)
			}
			val, used := protowire.ConsumeBytes(b)
			if used < 0 {
				return out, errors.New("malformed bytes")
			}
			switch field {
			case 1:
				out.Type = string(val)
			case 2:
				out.DeviceID = string(val)
			case 3:
				out.TaskID = string(val)
			case 4:
				out.Command = string(val)
			case 6:
				out.Stdout = append([]byte(nil), val...)
			case 7:
				out.Stderr = append([]byte(nil), val...)
			case 9:
				out.OS = string(val)
			case 10:
				out.Error = string(val)
			case 20:
				out.Capabilities = append([]byte(nil), val...)
			}
			b = b[used:]
		case 5, 8:
			if typ != protowire.VarintType {
				return out, fmt.Errorf("bad wire type for field %d", field)
			}
			val, used := protowire.ConsumeVarint(b)
			if used < 0 {
				return out, errors.New("malformed varint")
			}
			if field == 5 {
				out.ExitCode = int32(val)
			} else {
				out.TimeoutMS = uint32(val)
			}
			b = b[used:]
		default:
			used := protowire.ConsumeFieldValue(field, typ, b)
			if used < 0 {
				return out, errors.New("malformed unknown field")
			}
			b = b[used:]
		}
	}
	return out, nil
}
