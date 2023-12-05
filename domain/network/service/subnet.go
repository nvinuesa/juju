// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"

	"github.com/juju/errors"
	"github.com/juju/utils/v3"

	"github.com/juju/juju/core/network"
)

// SubnetState describes retrieval and persistence methods for subnet storage.
type SubnetState interface {
	// AddSubnet creates a subnet.
	AddSubnet(ctx context.Context, uuid utils.UUID, cidr string, providerID network.Id, providerNetworkID network.Id, VLANTag int, availabilityZones []string, spaceUUID string, fanInfo *network.FanCIDRs) error
	// GetAllSubnets returns all known subnets in the model.
	GetAllSubnets(ctx context.Context) (network.SubnetInfos, error)
	// GetSubnet returns the subnet by UUID.
	GetSubnet(ctx context.Context, uuid string) (*network.SubnetInfo, error)
	// GetSubnetsByCIDR returns the subnets by CIDR.
	// Deprecated, this method should be removed when we re-work the API
	// for moving subnets.
	GetSubnetsByCIDR(ctx context.Context, cidrs ...string) (network.SubnetInfos, error)
	// UpdateSubnet updates the subnet identified by the passed uuid.
	UpdateSubnet(ctx context.Context, uuid string, spaceID string) error
	// DeleteSubnet deletes the subnet identified by the passed uuid.
	DeleteSubnet(ctx context.Context, uuid string) error
}

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

// AddSubnet creates and returns a new subnet.
func (s *SubnetService) AddSubnet(ctx context.Context, args network.SubnetInfo) (*network.SubnetInfo, error) {
	uuid, err := utils.NewUUID()
	if err != nil {
		return nil, errors.Annotatef(err, "creating uuid for new subnet with CIDR %q", args.CIDR)
	}

	if err := s.st.AddSubnet(
		ctx,
		uuid,
		args.CIDR,
		args.ProviderId,
		args.ProviderNetworkId,
		args.VLANTag,
		args.AvailabilityZones,
		args.SpaceID,
		args.FanInfo,
	); err != nil {
		return nil, errors.Trace(err)
	}

	return s.st.GetSubnet(ctx, uuid.String())
}

// Subnet returns the subnet identified by the input UUID,
// or an error if it is not found.
func (s *SubnetService) Subnet(ctx context.Context, uuid string) (*network.SubnetInfo, error) {
	return s.st.GetSubnet(ctx, uuid)
}

// SubnetsByCIDR returns the subnets matching the input CIDRs.
func (s *SubnetService) SubnetsByCIDR(ctx context.Context, cidrs ...string) ([]network.SubnetInfo, error) {
	return s.st.GetSubnetsByCIDR(ctx, cidrs...)
}

// UpdateSubnet updates the spaceUUID of the subnet identified by the input
// UUID.
func (s *SubnetService) UpdateSubnet(ctx context.Context, uuid, spaceUUID string) error {
	return s.st.UpdateSubnet(ctx, uuid, spaceUUID)
}

// Remove deletes a subnet identified by its uuid.
func (s *SubnetService) Remove(ctx context.Context, uuid string) error {
	return s.st.DeleteSubnet(ctx, uuid)
}
