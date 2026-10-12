// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"github.com/juju/tc"
)

func (s *serviceSuite) TestBinaryArchiveRepresentationsAndUnchangedInput(c *tc.C) {
	type binaryRow struct {
		Value *[]byte `yaml:"value"`
	}
	for _, test := range []struct {
		yaml string
		want []byte
	}{
		{"!!binary AP8C", []byte{0, 255, 2}},
		{"[0, 255, 2]", []byte{0, 255, 2}},
		{"plain-text", []byte("plain-text")},
		{"[]", []byte{}},
	} {
		data := []byte("version: 4.1.0\npayload:\n  sample:\n  - value: " + test.yaml + "\n")
		parsed, err := decodeDump(data)
		c.Assert(err, tc.ErrorIsNil)
		original := parsed.Tables["sample"][0]["value"]
		for range 2 {
			decoded, err := decodeRows[binaryRow](parsed.Tables["sample"], "value")
			c.Assert(err, tc.ErrorIsNil)
			c.Assert(decoded, tc.HasLen, 1)
			c.Assert(decoded[0].Value, tc.NotNil)
			c.Check(*decoded[0].Value, tc.DeepEquals, test.want)
			c.Check(parsed.Tables["sample"][0]["value"], tc.DeepEquals, original)
		}
	}
}

func (s *serviceSuite) TestInvalidBinaryArchiveValues(c *tc.C) {
	type binaryRow struct {
		Value *[]byte `yaml:"value"`
	}
	for _, value := range []string{"[-1]", "[256]", "[1.5]", "true", "{invalid: value}"} {
		parsed, err := decodeDump([]byte("version: 4.1.0\npayload: {sample: [{value: " + value + "}]}"))
		c.Assert(err, tc.ErrorIsNil)
		_, err = decodeRows[binaryRow](parsed.Tables["sample"], "value")
		c.Assert(err, tc.NotNil)
	}
}
