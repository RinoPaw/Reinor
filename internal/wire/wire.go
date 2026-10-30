package wire

import (
	"errors"
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
)

// Screenshot data is bounded by the Agent before transmission.
const MaxFrame = 4 * 1024 * 1024

type Envelope struct {
	Type           string
	DeviceID       string
	TaskID         string
	Command        string
	ExitCode       int32
	Stdout         []byte
	Stderr         []byte
	TimeoutMS      uint32
	OS             string
	Error          string
	Data           []byte
	MIMEType       string
	Width          uint32
	Height         uint32
	OriginX        int32
	OriginY        int32
	PhysicalWidth  uint32
	PhysicalHeight uint32
	Quality        uint32
	Capabilities   string
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
	b = bytesField(b, 11, v.Data)
	b = str(b, 12, v.MIMEType)
	b = varint(b, 13, uint64(v.Width))
	b = varint(b, 14, uint64(v.Height))
	b = varint(b, 15, uint64(int64(v.OriginX)))
	b = varint(b, 16, uint64(int64(v.OriginY)))
	b = varint(b, 17, uint64(v.PhysicalWidth))
	b = varint(b, 18, uint64(v.PhysicalHeight))
	b = varint(b, 19, uint64(v.Quality))
	b = str(b, 20, v.Capabilities)
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
		case 1, 2, 3, 4, 6, 7, 9, 10, 11, 12, 20:
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
			case 11:
				out.Data = append([]byte(nil), val...)
			case 12:
				out.MIMEType = string(val)
			case 20:
				out.Capabilities = string(val)
			}
			b = b[used:]
		case 5, 8, 13, 14, 15, 16, 17, 18, 19:
			if typ != protowire.VarintType {
				return out, fmt.Errorf("bad wire type for field %d", field)
			}
			val, used := protowire.ConsumeVarint(b)
			if used < 0 {
				return out, errors.New("malformed varint")
			}
			switch field {
			case 5:
				out.ExitCode = int32(val)
			case 8:
				out.TimeoutMS = uint32(val)
			case 13:
				out.Width = uint32(val)
			case 14:
				out.Height = uint32(val)
			case 15:
				out.OriginX = int32(val)
			case 16:
				out.OriginY = int32(val)
			case 17:
				out.PhysicalWidth = uint32(val)
			case 18:
				out.PhysicalHeight = uint32(val)
			case 19:
				out.Quality = uint32(val)
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
