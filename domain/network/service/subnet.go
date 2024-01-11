// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"

	"github.com/juju/errors"
	"github.com/juju/utils/v3"

	"github.com/juju/juju/core/network"
)

// SubnetService provides the API for working with subnets.
type SubnetService struct {
	st SubnetState
}

// NewSunbetService returns a new service reference wrapping the input state.
func NewSubnetService(st SubnetState) *SubnetService {
	return &SubnetService{
		st: st,
	}
}

// AddSubnet creates a subnet and returns its ID.
func (s *SubnetService) AddSubnet(ctx context.Context, args network.SubnetInfo) (network.Id, error) {
	if args.ID == "" {
		uuid, err := utils.NewUUID()
		if err != nil {
			return "", errors.Annotatef(err, "creating uuid for new subnet with CIDR %q", args.CIDR)
		}
		args.ID = network.Id(uuid.String())
	}

	if err := s.st.AddSubnet(ctx, args); err != nil {
		return "", errors.Trace(err)
	}

	return args.ID, nil
}

// Subnet returns the subnet identified by the input UUID,
// or an error if it is not found.
func (s *SubnetService) Subnet(ctx context.Context, uuid string) (*network.SubnetInfo, error) {
	subnet, err := s.st.GetSubnet(ctx, uuid)
	return subnet, errors.Trace(err)
}

// SubnetsByCIDR returns the subnets matching the input CIDRs.
func (s *SubnetService) SubnetsByCIDR(ctx context.Context, cidrs ...string) ([]network.SubnetInfo, error) {
	subnets, err := s.st.GetSubnetsByCIDR(ctx, cidrs...)
	return subnets, errors.Trace(err)
}

// GetAllSubnets returns all subnets.
func (s *SubnetService) GetAllSubnets(ctx context.Context) (network.SubnetInfos, error) {
	subnet, err := s.st.GetAllSubnets(ctx)
	return subnet, errors.Trace(err)
}

// UpdateSubnet updates the spaceUUID of the subnet identified by the input
// UUID.
func (s *SubnetService) UpdateSubnet(ctx context.Context, uuid, spaceUUID string) error {
	return errors.Trace(s.st.UpdateSubnet(ctx, uuid, spaceUUID))
}

// Remove deletes a subnet identified by its uuid.
func (s *SubnetService) Remove(ctx context.Context, uuid string) error {
	return errors.Trace(s.st.DeleteSubnet(ctx, uuid))
}
