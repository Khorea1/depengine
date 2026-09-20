package planner

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/methodkind"
)

var crossMethodIntentKeys = map[string]struct{}{
	"architecture": {}, "branch": {}, "channel": {}, "digest": {}, "environment": {},
	"platform": {}, "prefix": {}, "registry": {}, "remote": {}, "rev": {}, "risk": {},
	"root": {}, "scope": {}, "source": {}, "tag": {}, "target": {}, "track": {}, "version": {},
}

func validateConfigKeys(cfg map[string]any, contract *methodkind.Contract) error {
	for key := range cfg {
		if strings.HasPrefix(key, "_") || declaredOrPortable(key, contract) {
			continue
		}
		return fmt.Errorf("field %q is not supported by method %q", key, contract.Kind)
	}
	return nil
}

func declaredOrPortable(key string, contract *methodkind.Contract) bool {
	if _, ok := contract.Fields[key]; ok {
		return true
	}
	_, ok := crossMethodIntentKeys[key]
	return ok
}
