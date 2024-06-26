// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"fmt"

	"github.com/juju/juju/domain/charm"
	internalcharm "github.com/juju/juju/internal/charm"
)

func decodeConfig(options charm.Config) (internalcharm.Config, error) {
	if len(options.Options) == 0 {
		return internalcharm.Config{}, nil
	}

	result := make(map[string]internalcharm.Option)
	for name, option := range options.Options {
		opt, err := decodeConfigOption(option)
		if err != nil {
			return internalcharm.Config{}, fmt.Errorf("decode config option: %w", err)
		}

		result[name] = opt
	}
	return internalcharm.Config{
		Options: result,
	}, nil
}

func decodeConfigOption(option charm.Option) (internalcharm.Option, error) {
	t, err := decodeOptionType(option.Type)
	if err != nil {
		return internalcharm.Option{}, fmt.Errorf("decode option type: %w", err)
	}

	return internalcharm.Option{
		Type:        t,
		Description: option.Description,
		Default:     option.Default,
	}, nil
}

func decodeOptionType(t charm.OptionType) (string, error) {
	switch t {
	case charm.OptionString:
		return "string", nil
	case charm.OptionInt:
		return "int", nil
	case charm.OptionFloat:
		return "float", nil
	case charm.OptionBool:
		return "boolean", nil
	case charm.OptionSecret:
		return "secret", nil
	default:
		return "", fmt.Errorf("unknown option type %q", t)
	}
}
