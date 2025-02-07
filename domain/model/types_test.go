// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package model

import (
	"github.com/juju/errors"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/core/credential"
	modeltesting "github.com/juju/juju/core/model/testing"
	usertesting "github.com/juju/juju/core/user/testing"
)

type typesSuite struct {
	testing.IsolationSuite
}

var _ = gc.Suite(&typesSuite{})

// TestModelCreationArgsValidation is aserting all the validation cases that the
// [GlobalModelCreationArgs.Validate] function checks for.
func (*typesSuite) TestModelCreationArgsValidation(c *gc.C) {
	userUUID := usertesting.GenUserUUID(c)

	tests := []struct {
		Args    GlobalModelCreationArgs
		Name    string
		ErrTest error
	}{
		{
			Name: "Test invalid name",
			Args: GlobalModelCreationArgs{
				Cloud:       "my-cloud",
				CloudRegion: "my-region",
				Name:        "",
				Owner:       userUUID,
			},
			ErrTest: errors.NotValid,
		},
		{
			Name: "Test invalid owner",
			Args: GlobalModelCreationArgs{
				Cloud:       "my-cloud",
				CloudRegion: "my-region",
				Name:        "my-awesome-model",
				Owner:       "",
			},
			ErrTest: errors.NotValid,
		},
		{
			Name: "Test invalid cloud",
			Args: GlobalModelCreationArgs{
				Cloud:       "",
				CloudRegion: "my-region",
				Name:        "my-awesome-model",
				Owner:       userUUID,
			},
			ErrTest: errors.NotValid,
		},
		{
			Name: "Test invalid cloud region",
			Args: GlobalModelCreationArgs{
				Cloud:       "my-cloud",
				CloudRegion: "",
				Name:        "my-awesome-model",
				Owner:       userUUID,
			},
			ErrTest: nil,
		},
		{
			Name: "Test invalid credential key",
			Args: GlobalModelCreationArgs{
				Cloud:       "my-cloud",
				CloudRegion: "my-region",
				Credential: credential.Key{
					Owner: usertesting.GenNewName(c, "wallyworld"),
				},
				Name:  "my-awesome-model",
				Owner: userUUID,
			},
			ErrTest: errors.NotValid,
		},
		{
			Name: "Test happy path without credential key",
			Args: GlobalModelCreationArgs{
				Cloud:       "my-cloud",
				CloudRegion: "my-region",
				Name:        "my-awesome-model",
				Owner:       userUUID,
			},
			ErrTest: nil,
		},
		{
			Name: "Test happy path with credential key",
			Args: GlobalModelCreationArgs{
				Cloud:       "my-cloud",
				CloudRegion: "my-region",
				Credential: credential.Key{
					Cloud: "cloud",
					Owner: usertesting.GenNewName(c, "wallyworld"),
					Name:  "mycred",
				},
				Name:  "my-awesome-model",
				Owner: userUUID,
			},
			ErrTest: nil,
		},
	}

	for i, test := range tests {
		c.Logf("testing %q: %d %v", test.Name, i, test.Args)

		err := test.Args.Validate()
		if test.ErrTest == nil {
			c.Check(err, jc.ErrorIsNil, gc.Commentf("%s", test.Name))
		} else {
			c.Check(err, jc.ErrorIs, test.ErrTest, gc.Commentf("%s", test.Name))
		}
	}
}

// TestModelImportArgsValidation is aserting all the validation cases that the
// [ModelImportArgs.Validate] function checks for.
func (*typesSuite) TestModelImportArgsValidation(c *gc.C) {
	userUUID := usertesting.GenUserUUID(c)

	tests := []struct {
		Args    ModelImportArgs
		Name    string
		ErrTest error
	}{
		{
			Name: "Test happy path with valid model id",
			Args: ModelImportArgs{
				GlobalModelCreationArgs: GlobalModelCreationArgs{
					Cloud:       "my-cloud",
					CloudRegion: "my-region",
					Credential: credential.Key{
						Cloud: "cloud",
						Owner: usertesting.GenNewName(c, "wallyworld"),
						Name:  "mycred",
					},
					Name:  "my-awesome-model",
					Owner: userUUID,
				},
				ID: modeltesting.GenModelUUID(c),
			},
		},
		{
			Name: "Test invalid model id",
			Args: ModelImportArgs{
				GlobalModelCreationArgs: GlobalModelCreationArgs{
					Cloud:       "my-cloud",
					CloudRegion: "my-region",
					Credential: credential.Key{
						Cloud: "cloud",
						Owner: usertesting.GenNewName(c, "wallyworld"),
						Name:  "mycred",
					},
					Name:  "my-awesome-model",
					Owner: userUUID,
				},
				ID: "not valid",
			},
			ErrTest: errors.NotValid,
		},
	}

	for i, test := range tests {
		c.Logf("testing %q: %d %v", test.Name, i, test.Args)

		err := test.Args.Validate()
		if test.ErrTest == nil {
			c.Check(err, jc.ErrorIsNil, gc.Commentf("%s", test.Name))
		} else {
			c.Check(err, jc.ErrorIs, test.ErrTest, gc.Commentf("%s", test.Name))
		}
	}
}
