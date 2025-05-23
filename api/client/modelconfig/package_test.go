// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelconfig

import (
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/api/base"
)

func Test(t *testing.T) {
	tc.TestingT(t)
}

func NewClientFromCaller(caller base.FacadeCaller) *Client {
	return &Client{
		facade: caller,
	}
}
