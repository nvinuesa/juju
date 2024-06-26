// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package changestream

var (
	// DefaultNumTermWatermarks is the default number of terms (watermarks) to
	// keep before removing the oldest one.
	DefaultNumTermWatermarks = 10
)
