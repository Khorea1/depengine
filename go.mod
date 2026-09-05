module github.com/Khorea1/depengine

go 1.26.4

require (
	github.com/pelletier/go-toml/v2 v2.2.4
	github.com/spf13/cobra v1.8.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
)

// gopkg.in/yaml.v3 and gopkg.in/check.v1 are transitive requirements of
// Cobra's go.mod (used only by its unimported cobra/doc subpackage and by
// yaml's own test suite, respectively — depengine never imports either).
// These replaces point at the same upstream source the gopkg.in vanity
// import paths redirect to, so `go mod tidy`/`go build` don't depend on the
// vanity-redirect service being reachable.
replace gopkg.in/yaml.v3 => github.com/go-yaml/yaml v3.0.1+incompatible

replace gopkg.in/check.v1 => github.com/go-check/check v0.0.0-20201130134442-10cb98267c6c
