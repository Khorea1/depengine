package run

import (
	"context"
	"errors"
	"testing"
)

// FuzzCapturedProcessOutput checks the central process-result boundary before
// callers parse subprocess stdout as a schema, version or inventory. In
// particular a successful exit with incomplete inherited pipes is not success.
func FuzzCapturedProcessOutput(f *testing.F) {
	for _, sample := range [][]byte{nil, {}, []byte("{}"), []byte("version=1.0\n"), []byte("\x00\xff\xfe"), []byte("token=topsecret")} {
		for mode := uint8(0); mode < 5; mode++ {
			f.Add(sample, mode)
		}
	}
	f.Fuzz(func(t *testing.T, output []byte, mode uint8) {
		if len(output) > 32<<10 {
			return
		}
		result := Result{Stdout: output, Stderr: output}
		switch mode % 5 {
		case 1:
			result.ExitCode = 1
		case 2:
			result.Err = context.Canceled
		case 3:
			result.WaitErr = errors.New("process pipe incomplete")
		case 4:
			result.Err = context.DeadlineExceeded
			result.WaitErr = errors.New("truncated")
		}
		err := CheckResultCompleteOutput(result, "external observation")
		if wantSuccess := mode%5 == 0; (err == nil) != wantSuccess {
			t.Fatalf("CheckResultCompleteOutput success=%t, want %t (mode=%d)", err == nil, wantSuccess, mode)
		}
	})
}
