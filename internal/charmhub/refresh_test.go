// Copyright 2020 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package charmhub

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/juju/collections/set"
	"github.com/juju/errors"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils/v4"
	"go.uber.org/mock/gomock"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/core/arch"
	charmmetrics "github.com/juju/juju/core/charm/metrics"
	"github.com/juju/juju/internal/charmhub/path"
	"github.com/juju/juju/internal/charmhub/transport"
)

type RefreshSuite struct {
	baseSuite
}

var (
	_ = gc.Suite(&RefreshSuite{})

	expRefreshFields = set.NewStrings(
		"download", "id", "license", "name", "publisher", "resources",
		"revision", "summary", "type", "version", "bases", "config-yaml",
		"metadata-yaml",
	).SortedValues()
)

func (s *RefreshSuite) TestRefresh(c *gc.C) {
	ctrl := gomock.NewController(c)
	defer ctrl.Finish()

	baseURL := MustParseURL(c, "http://api.foo.bar")

	baseURLPath := path.MakePath(baseURL)
	id := "meshuggah"
	body := transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{{
			InstanceKey: "instance-key",
			ID:          id,
			Revision:    1,
			Base: transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
			TrackingChannel: "latest/stable",
		}},
		Actions: []transport.RefreshRequestAction{{
			Action:      "refresh",
			InstanceKey: "instance-key",
			ID:          &id,
		}},
		Fields: expRefreshFields,
	}

	config, err := RefreshOne("instance-key", id, 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	restClient := NewMockRESTClient(ctrl)
	s.expectPost(restClient, baseURLPath, id, body)

	client := newRefreshClient(baseURLPath, restClient, s.logger)
	responses, err := client.Refresh(context.Background(), config)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(len(responses), gc.Equals, 1)
	c.Assert(responses[0].Name, gc.Equals, id)
}

// c.Assert(results.Results[0].Error, gc.ErrorMatches, `.* pool "foo" not found`)
func (s *RefreshSuite) TestRefeshConfigValidateArch(c *gc.C) {
	err := s.testRefeshConfigValidate(c, RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: "all",
	})
	c.Assert(err, gc.ErrorMatches, "Architecture.*")
}

func (s *RefreshSuite) TestRefeshConfigValidateSeries(c *gc.C) {
	err := s.testRefeshConfigValidate(c, RefreshBase{
		Name:         "ubuntu",
		Channel:      "all",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, gc.ErrorMatches, "Channel.*")
}

func (s *RefreshSuite) TestRefeshConfigVali914dateName(c *gc.C) {
	err := s.testRefeshConfigValidate(c, RefreshBase{
		Name:         "all",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, gc.ErrorMatches, "Name.*")
}

func (s *RefreshSuite) TestRefeshConfigValidate(c *gc.C) {
	err := s.testRefeshConfigValidate(c, RefreshBase{
		Name:         "all",
		Channel:      "all",
		Architecture: "all",
	})
	c.Assert(err, gc.ErrorMatches, "Architecture.*, Name.*, Channel.*")
}

func (s *RefreshSuite) testRefeshConfigValidate(c *gc.C, rp RefreshBase) error {
	_, err := DownloadOneFromChannel("meshuggah", "latest/stable", rp)
	c.Assert(err, jc.ErrorIs, errors.NotValid)
	return err
}

type metadataHTTPClient struct {
	requestHeaders http.Header
	responseBody   string
}

func (t *metadataHTTPClient) Do(req *http.Request) (*http.Response, error) {
	t.requestHeaders = req.Header
	resp := &http.Response{
		Status:        "200 OK",
		StatusCode:    200,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          io.NopCloser(bytes.NewBufferString(t.responseBody)),
		ContentLength: int64(len(t.responseBody)),
		Request:       req,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
	}
	return resp, nil
}

func (s *RefreshSuite) TestRefreshMetadata(c *gc.C) {
	baseURL := MustParseURL(c, "http://api.foo.bar")
	baseURLPath := path.MakePath(baseURL)

	httpClient := &metadataHTTPClient{
		responseBody: `
{
  "error-list": [],
  "results": [
    {
      "id": "foo",
      "instance-key": "instance-key-foo"
    },
    {
      "id": "bar",
      "instance-key": "instance-key-bar"
    }
  ]
}
`,
	}

	restClient := newHTTPRESTClient(httpClient)
	client := newRefreshClient(baseURLPath, restClient, s.logger)

	config1, err := RefreshOne("instance-key-foo", "foo", 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: "amd64",
	})
	c.Assert(err, jc.ErrorIsNil)

	config2, err := RefreshOne("instance-key-bar", "bar", 2, "latest/edge", RefreshBase{
		Name:         "ubuntu",
		Channel:      "14.04",
		Architecture: "amd64",
	})
	c.Assert(err, jc.ErrorIsNil)

	config := RefreshMany(config1, config2)

	response, err := client.Refresh(context.Background(), config)
	c.Assert(err, jc.ErrorIsNil)

	c.Assert(response, gc.DeepEquals, []transport.RefreshResponse{
		{ID: "foo", InstanceKey: "instance-key-foo"},
		{ID: "bar", InstanceKey: "instance-key-bar"},
	})
}

func (s *RefreshSuite) TestRefreshMetadataRandomOrder(c *gc.C) {
	baseURL := MustParseURL(c, "http://api.foo.bar")
	baseURLPath := path.MakePath(baseURL)

	httpClient := &metadataHTTPClient{
		responseBody: `
{
  "error-list": [],
  "results": [
    {
        "id": "bar",
        "instance-key": "instance-key-bar"
    },
    {
      "id": "foo",
      "instance-key": "instance-key-foo"
    }
  ]
}
`,
	}

	restClient := newHTTPRESTClient(httpClient)
	client := newRefreshClient(baseURLPath, restClient, s.logger)

	config1, err := RefreshOne("instance-key-foo", "foo", 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: "amd64",
	})
	c.Assert(err, jc.ErrorIsNil)

	config2, err := RefreshOne("instance-key-bar", "bar", 2, "latest/edge", RefreshBase{
		Name:         "ubuntu",
		Channel:      "14.04",
		Architecture: "amd64",
	})
	c.Assert(err, jc.ErrorIsNil)

	config := RefreshMany(config1, config2)

	response, err := client.Refresh(context.Background(), config)
	c.Assert(err, jc.ErrorIsNil)

	c.Assert(response, gc.DeepEquals, []transport.RefreshResponse{
		{ID: "foo", InstanceKey: "instance-key-foo"},
		{ID: "bar", InstanceKey: "instance-key-bar"},
	})
}

func (s *RefreshSuite) TestRefreshWithMetricsOnly(c *gc.C) {
	ctrl := gomock.NewController(c)
	defer ctrl.Finish()

	baseURL := MustParseURL(c, "http://api.foo.bar")

	baseURLPath := path.MakePath(baseURL)
	id := ""
	body := transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{},
		Metrics: map[string]map[string]string{
			"controller": {"uuid": "controller-uuid"},
			"model":      {"units": "3", "uuid": "model-uuid"},
		},
	}

	restClient := NewMockRESTClient(ctrl)
	s.expectPost(restClient, baseURLPath, id, body)

	metrics := Metrics{
		charmmetrics.Controller: {
			charmmetrics.UUID: "controller-uuid",
		},
		charmmetrics.Model: {
			charmmetrics.NumUnits: "3",
			charmmetrics.UUID:     "model-uuid",
		},
	}

	client := newRefreshClient(baseURLPath, restClient, s.logger)
	err := client.RefreshWithMetricsOnly(context.Background(), metrics)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *RefreshSuite) TestRefreshWithRequestMetrics(c *gc.C) {
	ctrl := gomock.NewController(c)
	defer ctrl.Finish()

	baseURL := MustParseURL(c, "http://api.foo.bar")

	baseURLPath := path.MakePath(baseURL)
	id := "meshuggah"
	body := transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{{
			InstanceKey: "instance-key-foo",
			ID:          id,
			Revision:    1,
			Base: transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
			TrackingChannel: "latest/stable",
		}, {
			InstanceKey: "instance-key-bar",
			ID:          id,
			Revision:    2,
			Base: transport.Base{
				Name:         "ubuntu",
				Channel:      "14.04",
				Architecture: arch.DefaultArchitecture,
			},
			TrackingChannel: "latest/edge",
		}},
		Actions: []transport.RefreshRequestAction{{
			Action:      "refresh",
			InstanceKey: "instance-key-foo",
			ID:          &id,
		}, {
			Action:      "refresh",
			InstanceKey: "instance-key-bar",
			ID:          &id,
		}},
		Fields: expRefreshFields,
		Metrics: map[string]map[string]string{
			"controller": {"uuid": "controller-uuid"},
			"model":      {"units": "3", "uuid": "model-uuid"},
		},
	}

	config1, err := RefreshOne("instance-key-foo", id, 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: "amd64",
	})
	c.Assert(err, jc.ErrorIsNil)

	config2, err := RefreshOne("instance-key-bar", id, 2, "latest/edge", RefreshBase{
		Name:         "ubuntu",
		Channel:      "14.04",
		Architecture: "amd64",
	})
	c.Assert(err, jc.ErrorIsNil)

	config := RefreshMany(config1, config2)

	restClient := NewMockRESTClient(ctrl)
	restClient.EXPECT().Post(gomock.Any(), baseURLPath, gomock.Any(), body, gomock.Any()).DoAndReturn(func(_ context.Context, _ path.Path, _ http.Header, _ any, r any) (restResponse, error) {
		responses := r.(*transport.RefreshResponses)
		responses.Results = []transport.RefreshResponse{{
			InstanceKey: "instance-key-foo",
			Name:        id,
		}, {
			InstanceKey: "instance-key-bar",
			Name:        id,
		}}
		return restResponse{StatusCode: http.StatusOK}, nil
	})

	metrics := Metrics{
		charmmetrics.Controller: {
			charmmetrics.UUID: "controller-uuid",
		},
		charmmetrics.Model: {
			charmmetrics.NumUnits: "3",
			charmmetrics.UUID:     "model-uuid",
		},
	}

	client := newRefreshClient(baseURLPath, restClient, s.logger)
	responses, err := client.RefreshWithRequestMetrics(context.Background(), config, metrics)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(len(responses), gc.Equals, 2)
	c.Assert(responses[0].Name, gc.Equals, id)
}

func (s *RefreshSuite) TestRefreshFailure(c *gc.C) {
	ctrl := gomock.NewController(c)
	defer ctrl.Finish()

	baseURL := MustParseURL(c, "http://api.foo.bar")

	baseURLPath := path.MakePath(baseURL)
	name := "meshuggah"

	config, err := RefreshOne("instance-key", name, 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	restClient := NewMockRESTClient(ctrl)
	s.expectPostFailure(restClient)

	client := newRefreshClient(baseURLPath, restClient, s.logger)
	_, err = client.Refresh(context.Background(), config)
	c.Assert(err, gc.Not(jc.ErrorIsNil))
}

func (s *RefreshSuite) expectPost(client *MockRESTClient, p path.Path, name string, body interface{}) {
	client.EXPECT().Post(gomock.Any(), p, gomock.Any(), body, gomock.Any()).Do(func(_ context.Context, _ path.Path, _ http.Header, _ any, r any) (restResponse, error) {
		responses := r.(*transport.RefreshResponses)
		responses.Results = []transport.RefreshResponse{{
			InstanceKey: "instance-key",
			Name:        name,
		}}
		return restResponse{StatusCode: http.StatusOK}, nil
	})
}

func (s *RefreshSuite) expectPostFailure(client *MockRESTClient) {
	client.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(restResponse{StatusCode: http.StatusInternalServerError}, errors.Errorf("boom"))
}

func DefineInstanceKey(c *gc.C, config RefreshConfig, key string) RefreshConfig {
	switch t := config.(type) {
	case refreshOne:
		t.instanceKey = key
		return t
	case executeOne:
		t.instanceKey = key
		return t
	case executeOneByRevision:
		t.instanceKey = key
		return t
	default:
		c.Fatalf("unexpected config %T", config)
	}
	return nil
}

type RefreshConfigSuite struct {
	testing.IsolationSuite
}

var _ = gc.Suite(&RefreshConfigSuite{})

func (s *RefreshConfigSuite) TestRefreshOneBuild(c *gc.C) {
	id := "foo"
	config, err := RefreshOne("instance-key", id, 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{{
			InstanceKey: "instance-key",
			ID:          "foo",
			Revision:    1,
			Base: transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
			TrackingChannel: "latest/stable",
		}},
		Actions: []transport.RefreshRequestAction{{
			Action:      "refresh",
			InstanceKey: "instance-key",
			ID:          &id,
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestRefreshOneWithBaseChannelRiskBuild(c *gc.C) {
	id := "foo"
	config, err := RefreshOne("instance-key", id, 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04/stable",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{{
			InstanceKey: "instance-key",
			ID:          "foo",
			Revision:    1,
			Base: transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
			TrackingChannel: "latest/stable",
		}},
		Actions: []transport.RefreshRequestAction{{
			Action:      "refresh",
			InstanceKey: "instance-key",
			ID:          &id,
		}},
		Fields: expRefreshFields,
	})

}

func (s *RefreshConfigSuite) TestRefreshOneBuildInstanceKeyCompatibility(c *gc.C) {
	id := "foo"
	config, err := RefreshOne("", id, 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	key := ExtractConfigInstanceKey(config)
	c.Assert(utils.IsValidUUIDString(key), jc.IsTrue)
}

func (s *RefreshConfigSuite) TestRefreshOneWithMetricsBuild(c *gc.C) {
	id := "foo"
	config, err := RefreshOne("instance-key", id, 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config, err = AddConfigMetrics(config, map[charmmetrics.MetricValueKey]string{
		charmmetrics.Provider:        "openstack",
		charmmetrics.NumApplications: "4",
	})
	c.Assert(err, jc.ErrorIsNil)

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{{
			InstanceKey: "instance-key",
			ID:          "foo",
			Revision:    1,
			Base: transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
			TrackingChannel: "latest/stable",
			Metrics: map[string]string{
				"provider":     "openstack",
				"applications": "4",
			},
		}},
		Actions: []transport.RefreshRequestAction{{
			Action:      "refresh",
			InstanceKey: "instance-key",
			ID:          &id,
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestRefreshOneFail(c *gc.C) {
	_, err := RefreshOne("instance-key", "", 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIs, errors.NotValid)
}

func (s *RefreshConfigSuite) TestRefreshOneEnsure(c *gc.C) {
	config, err := RefreshOne("instance-key", "foo", 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	err = config.Ensure([]transport.RefreshResponse{{
		InstanceKey: "instance-key",
	}})
	c.Assert(err, jc.ErrorIsNil)
}

func (s *RefreshConfigSuite) TestInstallOneFromRevisionBuild(c *gc.C) {
	revision := 1

	name := "foo"
	config, err := InstallOneFromRevision(name, revision)
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "install",
			InstanceKey: "foo-bar",
			Name:        &name,
			Revision:    &revision,
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestInstallOneFromRevisionFail(c *gc.C) {
	_, err := InstallOneFromRevision("", 1)
	c.Assert(err, jc.ErrorIs, errors.NotValid)
}

func (s *RefreshConfigSuite) TestInstallOneBuildRevisionResources(c *gc.C) {
	// Tests InstallOne by revision with specific resources.
	revision := 1

	name := "foo"
	config, err := InstallOneFromRevision(name, revision)
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")
	config, ok := AddResource(config, "testme", 3)
	c.Assert(ok, jc.IsTrue)

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "install",
			InstanceKey: "foo-bar",
			Name:        &name,
			Revision:    &revision,
			ResourceRevisions: []transport.RefreshResourceRevision{
				{Name: "testme", Revision: 3},
			},
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestAddResourceFail(c *gc.C) {
	config, err := RefreshOne("instance-key", "testingID", 7, "latest/edge", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)
	_, ok := AddResource(config, "testme", 3)
	c.Assert(ok, jc.IsFalse)
}

func (s *RefreshConfigSuite) TestInstallOneBuildChannel(c *gc.C) {
	channel := "latest/stable"

	name := "foo"
	config, err := InstallOneFromChannel(name, channel, RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "install",
			InstanceKey: "foo-bar",
			Name:        &name,
			Channel:     &channel,
			Base: &transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestInstallOneChannelFail(c *gc.C) {
	_, err := InstallOneFromChannel("", "stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIs, errors.NotValid)
}

func (s *RefreshConfigSuite) TestInstallOneWithPartialPlatform(c *gc.C) {
	channel := "latest/stable"

	name := "foo"
	config, err := InstallOneFromChannel(name, channel, RefreshBase{
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "install",
			InstanceKey: "foo-bar",
			Name:        &name,
			Channel:     &channel,
			Base: &transport.Base{
				Name:         notAvailable,
				Channel:      notAvailable,
				Architecture: arch.DefaultArchitecture,
			},
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestInstallOneWithMissingArch(c *gc.C) {
	channel := "latest/stable"

	name := "foo"
	config, err := InstallOneFromChannel(name, channel, RefreshBase{})
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	_, err = config.Build()
	c.Assert(err, jc.ErrorIs, errors.NotValid)
}

func (s *RefreshConfigSuite) TestInstallOneFromChannelEnsure(c *gc.C) {
	config, err := InstallOneFromChannel("foo", "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	err = config.Ensure([]transport.RefreshResponse{{
		InstanceKey: "foo-bar",
	}})
	c.Assert(err, jc.ErrorIsNil)
}

func (s *RefreshConfigSuite) TestInstallOneFromChannelFail(c *gc.C) {
	_, err := InstallOneFromChannel("foo", "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)
}

func (s *RefreshConfigSuite) TestDownloadOneFromRevisionBuild(c *gc.C) {
	rev := 4
	id := "foo"
	config, err := DownloadOneFromRevision(id, rev)
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "download",
			InstanceKey: "foo-bar",
			ID:          &id,
			Revision:    &rev,
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestDownloadOneFromRevisionFail(c *gc.C) {
	_, err := DownloadOneFromRevision("", 7)
	c.Assert(err, jc.ErrorIs, errors.NotValid)
}

func (s *RefreshConfigSuite) TestDownloadOneFromRevisionByNameBuild(c *gc.C) {
	rev := 4
	name := "foo"
	config, err := DownloadOneFromRevisionByName(name, rev)
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "download",
			InstanceKey: "foo-bar",
			Name:        &name,
			Revision:    &rev,
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestDownloadOneFromRevisionByNameFail(c *gc.C) {
	_, err := DownloadOneFromRevisionByName("", 7)
	c.Assert(err, jc.ErrorIs, errors.NotValid)
}

func (s *RefreshConfigSuite) TestDownloadOneFromChannelBuild(c *gc.C) {
	channel := "latest/stable"
	id := "foo"
	config, err := DownloadOneFromChannel(id, channel, RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "download",
			InstanceKey: "foo-bar",
			ID:          &id,
			Channel:     &channel,
			Base: &transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestDownloadOneFromChannelFail(c *gc.C) {
	_, err := DownloadOneFromChannel("", "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIs, errors.NotValid)
}

func (s *RefreshConfigSuite) TestDownloadOneFromChannelByNameBuild(c *gc.C) {
	channel := "latest/stable"
	name := "foo"
	config, err := DownloadOneFromChannelByName(name, channel, RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "download",
			InstanceKey: "foo-bar",
			Name:        &name,
			Channel:     &channel,
			Base: &transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestDownloadOneFromChannelByNameFail(c *gc.C) {
	_, err := DownloadOneFromChannel("", "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIs, errors.NotValid)
}

func (s *RefreshConfigSuite) TestDownloadOneFromChannelBuildK8s(c *gc.C) {
	channel := "latest/stable"
	id := "foo"
	config, err := DownloadOneFromChannel(id, channel, RefreshBase{
		Name:         "kubernetes",
		Channel:      "kubernetes",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{},
		Actions: []transport.RefreshRequestAction{{
			Action:      "download",
			InstanceKey: "foo-bar",
			ID:          &id,
			Channel:     &channel,
			Base: &transport.Base{
				Name:         "ubuntu",
				Channel:      "24.04",
				Architecture: arch.DefaultArchitecture,
			},
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestDownloadOneFromChannelEnsure(c *gc.C) {
	config, err := DownloadOneFromChannel("foo", "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config = DefineInstanceKey(c, config, "foo-bar")

	err = config.Ensure([]transport.RefreshResponse{{
		InstanceKey: "foo-bar",
	}})
	c.Assert(err, jc.ErrorIsNil)
}

func (s *RefreshConfigSuite) TestRefreshManyBuildContextNotNil(c *gc.C) {
	id1 := "foo"
	config1, err := DownloadOneFromRevision(id1, 1)
	c.Assert(err, jc.ErrorIsNil)
	config1 = DefineInstanceKey(c, config1, "foo-bar")

	id2 := "bar"
	config2, err := DownloadOneFromChannel(id2, "latest/edge", RefreshBase{
		Name:         "ubuntu",
		Channel:      "14.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)
	config2 = DefineInstanceKey(c, config2, "foo-baz")
	config := RefreshMany(config1, config2)

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req.Context, gc.NotNil)
}

func (s *RefreshConfigSuite) TestRefreshManyBuild(c *gc.C) {
	id1 := "foo"
	config1, err := RefreshOne("instance-key", id1, 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	id2 := "bar"
	config2, err := RefreshOne("instance-key2", id2, 2, "latest/edge", RefreshBase{
		Name:         "ubuntu",
		Channel:      "14.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	channel := "1/stable"

	name3 := "baz"
	config3, err := InstallOneFromChannel(name3, "1/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "19.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config3 = DefineInstanceKey(c, config3, "foo-taz")

	name4 := "forty-two"
	rev4 := 42
	config4, err := InstallOneFromRevision(name4, rev4)
	c.Assert(err, jc.ErrorIsNil)

	config4 = DefineInstanceKey(c, config4, "foo-two")

	config := RefreshMany(config1, config2, config3, config4)

	req, err := config.Build()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(req, gc.DeepEquals, transport.RefreshRequest{
		Context: []transport.RefreshRequestContext{{
			InstanceKey: "instance-key",
			ID:          "foo",
			Revision:    1,
			Base: transport.Base{
				Name:         "ubuntu",
				Channel:      "20.04",
				Architecture: arch.DefaultArchitecture,
			},
			TrackingChannel: "latest/stable",
		}, {
			InstanceKey: "instance-key2",
			ID:          "bar",
			Revision:    2,
			Base: transport.Base{
				Name:         "ubuntu",
				Channel:      "14.04",
				Architecture: arch.DefaultArchitecture,
			},
			TrackingChannel: "latest/edge",
		}},
		Actions: []transport.RefreshRequestAction{{
			Action:      "refresh",
			InstanceKey: "instance-key",
			ID:          &id1,
		}, {
			Action:      "refresh",
			InstanceKey: "instance-key2",
			ID:          &id2,
		}, {
			Action:      "install",
			InstanceKey: "foo-taz",
			Name:        &name3,
			Base: &transport.Base{
				Name:         "ubuntu",
				Channel:      "19.04",
				Architecture: arch.DefaultArchitecture,
			},
			Channel: &channel,
		}, {
			Action:      "install",
			InstanceKey: "foo-two",
			Name:        &name4,
			Revision:    &rev4,
		}},
		Fields: expRefreshFields,
	})
}

func (s *RefreshConfigSuite) TestRefreshManyEnsure(c *gc.C) {
	config1, err := RefreshOne("instance-key", "foo", 1, "latest/stable", RefreshBase{
		Name:         "ubuntu",
		Channel:      "20.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config2, err := RefreshOne("instance-key2", "bar", 2, "latest/edge", RefreshBase{
		Name:         "ubuntu",
		Channel:      "14.04",
		Architecture: arch.DefaultArchitecture,
	})
	c.Assert(err, jc.ErrorIsNil)

	config := RefreshMany(config1, config2)

	err = config.Ensure([]transport.RefreshResponse{{
		InstanceKey: "instance-key",
	}, {
		InstanceKey: "instance-key2",
	}})
	c.Assert(err, jc.ErrorIsNil)
}
