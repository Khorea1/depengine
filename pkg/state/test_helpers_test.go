package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func writeChecksummedStateForTest(t *testing.T, path string, st State) {
	t.Helper()
	st.Checksum = ""
	canonical, err := json.Marshal(&st)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	st.Checksum = hex.EncodeToString(sum[:])
	data, err := json.Marshal(&st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
