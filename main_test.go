package main

import (
	"os"
	"testing"

	"github.com/Khorea1/depengine/internal/app"
)

func TestMain(m *testing.M) {
	app.InitAdapters()
	os.Exit(m.Run())
}
