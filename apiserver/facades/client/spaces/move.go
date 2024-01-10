// Copyright 2020 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package spaces

import (
	"context"

	"github.com/juju/collections/set"
	"github.com/juju/errors"
	"github.com/juju/names/v5"

	apiservererrors "github.com/juju/juju/apiserver/errors"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/rpc/params"
	"github.com/juju/juju/state"
)

// MoveSubnets ensures that the input subnets are in the input space.
// NOTE(nvinuesa): this method is not transactional, it applies each change
// on each subnet independently.
func (api *API) MoveSubnets(ctx context.Context, args params.MoveSubnetsParams) (params.MoveSubnetsResults, error) {
	var result params.MoveSubnetsResults

	if err := api.ensureSpacesAreMutable(ctx); err != nil {
		return result, err
	}

	results := make([]params.MoveSubnetsResult, len(args.Args))
OUTER:
	for i, toSpaceParams := range args.Args {
		// Note that although spaces have an ID, a space tag represents
		// a space *name*, which remains a unique identifier.
		// We need to retrieve the space in order to use its ID.
		spaceTag, err := names.ParseSpaceTag(toSpaceParams.SpaceTag)
		if err != nil {
			results[i].Error = apiservererrors.ServerError(errors.Trace(err))
			continue
		}
		spaceName := spaceTag.Id()

		subnets, err := api.getMovingSubnets(ctx, toSpaceParams.SubnetTags)
		if err != nil {
			results[i].Error = apiservererrors.ServerError(errors.Trace(err))
			continue
		}

		if err := api.ensureSubnetsCanBeMoved(subnets, spaceName, toSpaceParams.Force); err != nil {
			results[i].Error = apiservererrors.ServerError(errors.Trace(err))
			continue
		}

		space, err := api.spaceService.SpaceByName(ctx, spaceName)
		if err != nil {
			results[i].Error = apiservererrors.ServerError(errors.Trace(err))
			continue
		}

		for _, subnet := range subnets {
			err := api.subnetService.UpdateSubnet(ctx, subnet.ID.String(), space.ID)
			if err != nil {
				results[i].Error = apiservererrors.ServerError(errors.Trace(err))
				continue OUTER
			}
		}

		results[i].NewSpaceTag = spaceTag.String()
		results[i].MovedSubnets = paramsFromMovedSubnet(spaceName, subnets)
	}

	result.Results = results
	return result, nil
}

// getMovingSubnets acquires all the subnets that we have
// been requested to relocate, identified by their tags.
func (api *API) getMovingSubnets(ctx context.Context, tags []string) ([]network.SubnetInfo, error) {
	subnets := make([]network.SubnetInfo, len(tags))
	for i, tag := range tags {
		subTag, err := names.ParseSubnetTag(tag)
		if err != nil {
			return nil, errors.Trace(err)
		}
		subnet, err := api.subnetService.Subnet(ctx, subTag.Id())
		if err != nil {
			return nil, errors.Trace(err)
		}
		subnets[i] = *subnet
	}
	return subnets, nil
}

// ensureSubnetsCanBeMoved gathers the relevant networking info required to
// determine the validity of constraints and endpoint bindings resulting from
// a relocation of subnets.
// An error is returned if validity is violated and force is passed as false.
func (api *API) ensureSubnetsCanBeMoved(subnets []network.SubnetInfo, spaceName string, force bool) error {
	for _, subnet := range subnets {
		if subnet.FanLocalUnderlay() != "" {
			return errors.Errorf("subnet %q is a fan overlay of %q and cannot be moved; move the underlay instead",
				subnet.CIDR, subnet.FanLocalUnderlay())
		}
	}

	affected, err := api.getAffectedNetworks(subnets, spaceName, force)
	if err != nil {
		return errors.Annotate(err, "determining affected networks")
	}

	if err := api.ensureSpaceConstraintIntegrity(affected); err != nil {
		return errors.Trace(err)
	}

	return errors.Trace(api.ensureEndpointBindingsIntegrity(affected))
}

// getAffectedNetworks interrogates machines connected to moving subnets.
// From these it generates lists of common unit/subnet-topologies,
// grouped by application.
func (api *API) getAffectedNetworks(subnets []network.SubnetInfo, spaceName string, force bool) (*affectedNetworks, error) {
	movingSubnetIDs := network.MakeIDSet()
	for _, subnet := range subnets {
		movingSubnetIDs.Add(network.Id(subnet.ID))
	}

	allSpaces, err := api.backing.AllSpaceInfos()
	if err != nil {
		return nil, errors.Trace(err)
	}

	affected, err := newAffectedNetworks(movingSubnetIDs, spaceName, allSpaces, force, api.logger)
	if err != nil {
		return nil, errors.Trace(err)
	}

	machines, err := api.backing.AllMachines()
	if err != nil {
		return nil, errors.Trace(err)
	}

	if err := affected.processMachines(machines); err != nil {
		return nil, errors.Annotate(err, "processing machine networks")
	}

	return affected, nil
}

// ensureSpaceConstraintIntegrity identifies all applications connected to
// subnets that we have been asked to move.
// It then compares any space constraints that these applications have against
// the requested destination space, to check if they will have continuity of
// those constraints after subnet relocation.
// If force is true we only log a warning for violations, otherwise an error
// is returned.
func (api *API) ensureSpaceConstraintIntegrity(affected *affectedNetworks) error {
	constraints, err := api.backing.AllConstraints()
	if err != nil {
		return errors.Trace(err)
	}

	// Create a lookup of constrained space names by application.
	spaceConsByApp := make(map[string]set.Strings)
	for _, cons := range constraints {
		// Get the tag for the entity to which this constraint applies.
		tag := state.TagFromDocID(cons.ID())
		if tag == nil {
			return errors.Errorf("unable to determine an entity to which constraint %q applies", cons.ID())
		}

		// We only care if this is an application constraint,
		// and it includes spaces.
		val := cons.Value()
		if tag.Kind() == names.ApplicationTagKind && val.HasSpaces() {
			spaceCons := val.Spaces
			spaceConsByApp[tag.Id()] = set.NewStrings(*spaceCons...)
		}
	}

	return errors.Trace(affected.ensureConstraintIntegrity(spaceConsByApp))
}

func (api *API) ensureEndpointBindingsIntegrity(affected *affectedNetworks) error {
	allBindings, err := api.backing.AllEndpointBindings()
	if err != nil {
		return errors.Trace(err)
	}

	return errors.Trace(affected.ensureBindingsIntegrity(allBindings))
}

func paramsFromMovedSubnet(spaceName string, movedSubnets []network.SubnetInfo) []params.MovedSubnet {
	results := make([]params.MovedSubnet, len(movedSubnets))
	for i, v := range movedSubnets {
		results[i] = params.MovedSubnet{
			SubnetTag:   v.ID.String(),
			OldSpaceTag: names.NewSpaceTag(spaceName).String(),
			CIDR:        v.CIDR,
		}
	}
	return results
}
