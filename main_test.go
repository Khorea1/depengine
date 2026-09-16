package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	initAdapters()
	os.Exit(m.Run())
}
