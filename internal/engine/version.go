package engine

import "github.com/Khorea1/depengine/internal/platform"

func CompareVersion(a, b string) int { return platform.CompareVersion(a, b) }
