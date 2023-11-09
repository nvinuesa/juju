// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	ctx "context"

	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils/v3"
	gc "gopkg.in/check.v1"

	schematesting "github.com/juju/juju/domain/schema/testing"
)

type stateSuite struct {
	schematesting.ModelSuite
}

var _ = gc.Suite(&stateSuite{})

func (s *stateSuite) TestInvalidNameAddSpace(c *gc.C) {
	st := NewState(s.TxnRunnerFactory())

	uuid, err := utils.NewUUID()
	c.Assert(err, jc.ErrorIsNil)
	err = st.AddSpace(ctx.Background(), uuid, "invalid/space", "foo", []string{}, true)
	c.Assert(err, gc.ErrorMatches, "invalid space name.*")
}

func (s *stateSuite) TestAddSpace(c *gc.C) {
	st := NewState(s.TxnRunnerFactory())

	uuid, err := utils.NewUUID()
	c.Assert(err, jc.ErrorIsNil)
	subnets := []string{"subnet0", "subnet1"}
	err = st.AddSpace(ctx.Background(), uuid, "space0", "foo", subnets, true)
	c.Assert(err, jc.ErrorIsNil)

	db := s.DB()
	// Check the space entity.
	row := db.QueryRow("SELECT name,is_public FROM space WHERE uuid = ?", uuid.String())
	c.Assert(row.Err(), jc.ErrorIsNil)
	var name string
	var isPublic bool
	err = row.Scan(&name, &isPublic)
	c.Assert(err, jc.ErrorIsNil)
	c.Check(name, gc.Equals, "space0")
	c.Check(isPublic, gc.Equals, true)
	// Check the provider id for that space.
	row = db.QueryRow("SELECT provider_id FROM provider_space WHERE space_uuid = ?", uuid.String())
	c.Assert(row.Err(), jc.ErrorIsNil)
	var providerID string
	err = row.Scan(&providerID)
	c.Assert(err, jc.ErrorIsNil)
	c.Check(providerID, gc.Equals, "foo")
	// Check the subnet ids for that space.
	rows, err := db.Query("SELECT uuid FROM subnet WHERE space_uuid = ?", uuid.String())
	c.Assert(err, jc.ErrorIsNil)
	i := 0
	for rows.Next() {
		var subnetID string
		err = rows.Scan(&subnetID)
		c.Assert(err, jc.ErrorIsNil)
		c.Check(subnetID, gc.Equals, subnets[i])
		i++
	}
}
