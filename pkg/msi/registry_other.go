//go:build !windows

package msi

type unavailableRegistry struct{}

func newProductFinder() productFinder                                 { return unavailableRegistry{} }
func (unavailableRegistry) Find(string, string) (string, bool, error) { return "", false, nil }
