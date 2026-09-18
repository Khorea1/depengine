package msi

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

type fakeProducts struct {
	code      string
	ok        bool
	name      string
	publisher string
}

func (f *fakeProducts) Find(name, publisher string) (string, bool, error) {
	f.name = name
	f.publisher = publisher
	return f.code, f.ok, nil
}

func TestAdapterUsesProductRegistryIdentityFields(t *testing.T) {
	products := &fakeProducts{code: "{ABC-123}", ok: true}
	adapter := NewAdapter()
	adapter.products = products
	mc := &config.MethodCandidate{Config: map[string]any{
		"product_name": "Neovim",
		"publisher":    "Neovim Project",
	}}

	if !adapter.Check(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "nvim"}, mc) {
		t.Fatal("Check should report the product returned by the registry finder")
	}
	if products.name != "Neovim" || products.publisher != "Neovim Project" {
		t.Fatalf("registry lookup identity = (%q, %q), want (%q, %q)", products.name, products.publisher, "Neovim", "Neovim Project")
	}
}
