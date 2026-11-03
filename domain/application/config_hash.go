// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package application

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
)

// ConfigHash computes the persisted configuration hash, including trust.
// Recovery uses the same representation when resetting archived approvals.
func ConfigHash(config map[string]string, trust bool) string {
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	hash := sha256.New()
	for _, key := range keys {
		hash.Write([]byte(key))
		hash.Write([]byte(config[key]))
	}
	hash.Write([]byte(strconv.FormatBool(trust)))
	return hex.EncodeToString(hash.Sum(nil))
}
