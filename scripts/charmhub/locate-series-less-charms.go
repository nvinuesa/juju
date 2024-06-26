// Copyright 2021 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package main

import (
	"context"
	"fmt"
	"log"

	"gopkg.in/yaml.v3"

	"github.com/juju/juju/internal/charmhub"
	internallogger "github.com/juju/juju/internal/logger"
)

// The following program attempts to locate series-less charms on charmhub.
// These charms will not have a series or a map of containers.
func main() {
	client, err := charmhub.NewClient(charmhub.Config{
		Logger: internallogger.GetLogger("series"),
	})
	if err != nil {
		log.Fatal(err)
	}
	results, err := client.Find(context.TODO(), "")
	if err != nil {
		log.Fatal(err)
	}

	type metadata struct {
		Deployment map[any]any `json:"Deployment"`
	}

	for _, result := range results {
		if result.Type == "bundle" {
			continue
		}

		fmt.Println("Checking", result.Name)

		info, err := client.Info(context.TODO(), result.Name)
		if err != nil {
			log.Fatal(err)
		}

		var meta metadata
		if err := yaml.Unmarshal([]byte(info.DefaultRelease.Revision.MetadataYAML), &meta); err != nil {
			log.Fatal(err)
		}

		if len(meta.Deployment) != 0 {
			fmt.Println("FOUND!", result.Name, meta.Deployment)
		}
	}
}
