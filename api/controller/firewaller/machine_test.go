// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package firewaller_test

import (
	"context"

	"github.com/juju/names/v6"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	basetesting "github.com/juju/juju/api/base/testing"
	"github.com/juju/juju/api/controller/firewaller"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/life"
	coretesting "github.com/juju/juju/internal/testing"
	"github.com/juju/juju/rpc/params"
)

type machineSuite struct {
	coretesting.BaseSuite
}

var _ = gc.Suite(&machineSuite{})

func (s *machineSuite) TestMachine(c *gc.C) {
	apiCaller := basetesting.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		c.Check(objType, gc.Equals, "Firewaller")
		c.Check(version, gc.Equals, 0)
		c.Check(id, gc.Equals, "")
		c.Check(request, gc.Equals, "Life")
		c.Assert(arg, jc.DeepEquals, params.Entities{
			Entities: []params.Entity{{Tag: "machine-666"}},
		})
		c.Assert(result, gc.FitsTypeOf, &params.LifeResults{})
		*(result.(*params.LifeResults)) = params.LifeResults{
			Results: []params.LifeResult{{Life: "alive"}},
		}
		return nil
	})
	tag := names.NewMachineTag("666")
	client, err := firewaller.NewClient(apiCaller)
	c.Assert(err, jc.ErrorIsNil)
	m, err := client.Machine(context.Background(), tag)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(m.Life(), gc.Equals, life.Alive)
	c.Assert(m.Tag(), jc.DeepEquals, tag)
}

func (s *machineSuite) TestInstanceId(c *gc.C) {
	calls := 0
	apiCaller := basetesting.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		c.Check(objType, gc.Equals, "Firewaller")
		c.Check(version, gc.Equals, 0)
		c.Check(id, gc.Equals, "")
		c.Assert(arg, jc.DeepEquals, params.Entities{
			Entities: []params.Entity{{Tag: "machine-666"}},
		})
		if calls == 0 {
			c.Check(request, gc.Equals, "Life")
			c.Assert(result, gc.FitsTypeOf, &params.LifeResults{})
			*(result.(*params.LifeResults)) = params.LifeResults{
				Results: []params.LifeResult{{Life: "alive"}},
			}
		} else {
			c.Check(request, gc.Equals, "InstanceId")
			c.Assert(result, gc.FitsTypeOf, &params.StringResults{})
			*(result.(*params.StringResults)) = params.StringResults{
				Results: []params.StringResult{{Result: "inst-666"}},
			}
		}
		calls++
		return nil
	})
	tag := names.NewMachineTag("666")
	client, err := firewaller.NewClient(apiCaller)
	c.Assert(err, jc.ErrorIsNil)
	m, err := client.Machine(context.Background(), tag)
	c.Assert(err, jc.ErrorIsNil)
	id, err := m.InstanceId(context.Background())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(m.Life(), gc.Equals, life.Alive)
	c.Assert(id, gc.Equals, instance.Id("inst-666"))
	c.Assert(calls, gc.Equals, 2)
}

func (s *machineSuite) TestWatchUnits(c *gc.C) {
	calls := 0
	apiCaller := basetesting.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		c.Check(objType, gc.Equals, "Firewaller")
		c.Check(version, gc.Equals, 0)
		c.Check(id, gc.Equals, "")
		c.Assert(arg, jc.DeepEquals, params.Entities{
			Entities: []params.Entity{{Tag: "machine-666"}},
		})
		if calls > 0 {
			c.Assert(result, gc.FitsTypeOf, &params.StringsWatchResults{})
			c.Check(request, gc.Equals, "WatchUnits")
			*(result.(*params.StringsWatchResults)) = params.StringsWatchResults{
				Results: []params.StringsWatchResult{{Error: &params.Error{Message: "FAIL"}}},
			}
		} else {
			c.Assert(result, gc.FitsTypeOf, &params.LifeResults{})
			c.Check(request, gc.Equals, "Life")
			*(result.(*params.LifeResults)) = params.LifeResults{
				Results: []params.LifeResult{{Life: life.Alive}},
			}
		}
		calls++
		return nil
	})
	tag := names.NewMachineTag("666")
	client, err := firewaller.NewClient(apiCaller)
	c.Assert(err, jc.ErrorIsNil)
	m, err := client.Machine(context.Background(), tag)
	c.Assert(err, jc.ErrorIsNil)
	_, err = m.WatchUnits(context.Background())
	c.Assert(err, gc.ErrorMatches, "FAIL")
	c.Assert(calls, gc.Equals, 2)
}

func (s *machineSuite) TestIsManual(c *gc.C) {
	calls := 0
	apiCaller := basetesting.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		c.Check(objType, gc.Equals, "Firewaller")
		c.Check(version, gc.Equals, 0)
		c.Check(id, gc.Equals, "")
		c.Assert(arg, jc.DeepEquals, params.Entities{
			Entities: []params.Entity{{Tag: "machine-666"}},
		})
		if calls > 0 {
			c.Assert(result, gc.FitsTypeOf, &params.BoolResults{})
			c.Check(request, gc.Equals, "AreManuallyProvisioned")
			*(result.(*params.BoolResults)) = params.BoolResults{
				Results: []params.BoolResult{{Result: true}},
			}
		} else {
			c.Assert(result, gc.FitsTypeOf, &params.LifeResults{})
			c.Check(request, gc.Equals, "Life")
			*(result.(*params.LifeResults)) = params.LifeResults{
				Results: []params.LifeResult{{Life: life.Alive}},
			}
		}
		calls++
		return nil
	})
	tag := names.NewMachineTag("666")
	client, err := firewaller.NewClient(apiCaller)
	c.Assert(err, jc.ErrorIsNil)
	m, err := client.Machine(context.Background(), tag)
	c.Assert(err, jc.ErrorIsNil)
	result, err := m.IsManual(context.Background())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result, jc.IsTrue)
	c.Assert(calls, gc.Equals, 2)
}
