package evaluator

import "github.com/driftappdev/platform/featureflag/types"

func Enabled(flag types.Flag, _ types.Target) bool {
	return flag.Enabled
}


