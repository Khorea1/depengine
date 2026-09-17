package msi

import "testing"

type fakeProducts struct {
	code string
	ok   bool
}

func (f fakeProducts) Find(string, string) (string, bool, error) { return f.code, f.ok, nil }

func TestAdapterUsesProductRegistry(t *testing.T) {
	adapter := NewAdapter()
	adapter.products = fakeProducts{code: "{ABC-123}", ok: true}
	code, ok, err := adapter.products.Find("Neovim", "Neovim")
	if err != nil || !ok || code != "{ABC-123}" {
		t.Fatalf("code=%q ok=%v err=%v", code, ok, err)
	}
}
