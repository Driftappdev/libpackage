package evaluator

import "github.com/platformcore/libpackage/platform/featureflag/types"

func Enabled(flag types.Flag, _ types.Target) bool {
	return flag.Enabled
}


