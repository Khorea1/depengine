package engine

import _ "embed"

// detectScriptContent is the compiled-in copy of detect_os.sh.
// It is used as the primary detection path when available.
//
//go:embed detect_os.sh
var detectScriptContent []byte
