package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeExecuteCommand(t *testing.T) {
	cases := []struct {
		name    string
		command string
		encoded bool
		want    string
		wantErr bool
	}{
		{name: "plain default", command: "Get-Service MySQL80", want: "Get-Service MySQL80"},
		{name: "plain even if not Base64", command: "SGVsbG8=", want: "SGVsbG8="},
		{name: "encoded ASCII", command: base64.StdEncoding.EncodeToString([]byte("Get-Service MySQL80")), encoded: true, want: "Get-Service MySQL80"},
		{name: "encoded Unicode multiline", command: base64.StdEncoding.EncodeToString([]byte("Write-Output '测试'\nWrite-Output 'ok'")), encoded: true, want: "Write-Output '测试'\nWrite-Output 'ok'"},
		{name: "encoded exact limit", command: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 4096))), encoded: true, want: strings.Repeat("x", 4096)},
		{name: "invalid base64 alphabet", command: "not?base64", encoded: true, wantErr: true},
		{name: "invalid padding", command: "YWJj=", encoded: true, wantErr: true},
		{name: "invalid UTF-8", command: base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe}), encoded: true, wantErr: true},
		{name: "NUL byte", command: base64.StdEncoding.EncodeToString([]byte{'x', 0}), encoded: true, wantErr: true},
		{name: "empty", encoded: true, wantErr: true},
		{name: "decoded too long", command: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 4097))), encoded: true, wantErr: true},
		{name: "encoded input too long", command: strings.Repeat("A", 9000), encoded: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeExecuteCommand(execArgs{Command: tc.command, Base64: tc.encoded})
			if (err != nil) != tc.wantErr {
				t.Fatalf("decodeExecuteCommand() err=%v, wantErr=%t", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("decoded command mismatch: length=%d, expected length=%d", len(got), len(tc.want))
			}
		})
	}
}
