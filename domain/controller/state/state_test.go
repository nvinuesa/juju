// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"context"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	coremodel "github.com/juju/juju/core/model"
	schematesting "github.com/juju/juju/domain/schema/testing"
	jujutesting "github.com/juju/juju/internal/testing"
)

type stateSuite struct {
	schematesting.ControllerSuite
	controllerUUID      string
	controllerModelUUID coremodel.UUID
}

var _ = gc.Suite(&stateSuite{})

func (s *stateSuite) SetUpTest(c *gc.C) {
	s.controllerModelUUID = coremodel.UUID(jujutesting.ModelTag.Id())
	s.ControllerSuite.SetUpTest(c)
	s.controllerUUID = s.ControllerSuite.SeedControllerTable(c, s.controllerModelUUID)
}

func (s *stateSuite) TestControllerRetrieve(c *gc.C) {
	st := NewState(s.TxnRunnerFactory())
	uuid, modelUUID, err := st.Controller(context.Background())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(uuid, gc.Equals, s.controllerUUID)
	c.Assert(modelUUID, gc.Equals, s.controllerModelUUID)
}
