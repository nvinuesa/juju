// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provider_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/juju/errors"
	"github.com/juju/names/v6"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/internal/storage"
	"github.com/juju/juju/internal/storage/provider"
	"github.com/juju/juju/internal/testing"
)

var _ = gc.Suite(&loopSuite{})

type loopSuite struct {
	testing.BaseSuite
	storageDir string
	commands   *mockRunCommand
}

func (s *loopSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.storageDir = c.MkDir()
}

func (s *loopSuite) TearDownTest(c *gc.C) {
	s.commands.assertDrained()
	s.BaseSuite.TearDownTest(c)
}

func (s *loopSuite) loopProvider(c *gc.C) storage.Provider {
	s.commands = &mockRunCommand{c: c}
	return provider.LoopProvider(s.commands.run)
}

func (s *loopSuite) TestVolumeSource(c *gc.C) {
	p := s.loopProvider(c)
	cfg, err := storage.NewConfig("name", provider.LoopProviderType, map[string]interface{}{})
	c.Assert(err, jc.ErrorIsNil)
	_, err = p.VolumeSource(cfg)
	c.Assert(err, gc.ErrorMatches, "storage directory not specified")
	cfg, err = storage.NewConfig("name", provider.LoopProviderType, map[string]interface{}{
		"storage-dir": c.MkDir(),
	})
	c.Assert(err, jc.ErrorIsNil)
	_, err = p.VolumeSource(cfg)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *loopSuite) TestValidateConfig(c *gc.C) {
	p := s.loopProvider(c)
	cfg, err := storage.NewConfig("name", provider.LoopProviderType, map[string]interface{}{})
	c.Assert(err, jc.ErrorIsNil)
	err = p.ValidateConfig(cfg)
	// The loop provider does not have any user
	// configuration, so an empty map will pass.
	c.Assert(err, jc.ErrorIsNil)
}

func (s *loopSuite) TestSupports(c *gc.C) {
	p := s.loopProvider(c)
	c.Assert(p.Supports(storage.StorageKindBlock), jc.IsTrue)
	c.Assert(p.Supports(storage.StorageKindFilesystem), jc.IsFalse)
}

func (s *loopSuite) TestScope(c *gc.C) {
	p := s.loopProvider(c)
	c.Assert(p.Scope(), gc.Equals, storage.ScopeMachine)
}

func (s *loopSuite) loopVolumeSource(c *gc.C) (storage.VolumeSource, *provider.MockDirFuncs) {
	s.commands = &mockRunCommand{c: c}
	return provider.LoopVolumeSource(
		c.MkDir(),
		s.storageDir,
		s.commands.run,
	)
}

func (s *loopSuite) TestCreateVolumes(c *gc.C) {
	source, _ := s.loopVolumeSource(c)
	s.commands.expect("fallocate", "-l", "2MiB", filepath.Join(s.storageDir, "volume-0"))

	results, err := source.CreateVolumes(context.Background(), []storage.VolumeParams{{
		Tag:  names.NewVolumeTag("0"),
		Size: 2,
		Attachment: &storage.VolumeAttachmentParams{
			AttachmentParams: storage.AttachmentParams{
				Machine:    names.NewMachineTag("1"),
				InstanceId: "instance-id",
			},
		},
	}})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, gc.HasLen, 1)
	c.Assert(results[0].Error, jc.ErrorIsNil)
	// volume attachments always deferred to AttachVolumes
	c.Assert(results[0].VolumeAttachment, gc.IsNil)
	c.Assert(results[0].Volume, jc.DeepEquals, &storage.Volume{
		names.NewVolumeTag("0"),
		storage.VolumeInfo{
			VolumeId: "volume-0",
			Size:     2,
		},
	})
}

func (s *loopSuite) TestCreateVolumesNoAttachment(c *gc.C) {
	source, _ := s.loopVolumeSource(c)
	s.commands.expect("fallocate", "-l", "2MiB", filepath.Join(s.storageDir, "volume-0"))
	_, err := source.CreateVolumes(context.Background(), []storage.VolumeParams{{
		Tag:  names.NewVolumeTag("0"),
		Size: 2,
	}})
	// loop volumes may be created without attachments
	c.Assert(err, jc.ErrorIsNil)
}

func (s *loopSuite) TestDestroyVolumes(c *gc.C) {
	source, _ := s.loopVolumeSource(c)
	fileName := filepath.Join(s.storageDir, "volume-0")

	err := os.WriteFile(fileName, nil, 0644)
	c.Assert(err, jc.ErrorIsNil)

	errs, err := source.DestroyVolumes(context.Background(), []string{"volume-0"})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(errs, gc.HasLen, 1)
	c.Assert(errs[0], jc.ErrorIsNil)

	_, err = os.Stat(fileName)
	c.Assert(err, jc.Satisfies, os.IsNotExist)
}

func (s *loopSuite) TestDestroyVolumesInvalidVolumeId(c *gc.C) {
	source, _ := s.loopVolumeSource(c)
	errs, err := source.DestroyVolumes(context.Background(), []string{"../super/important/stuff"})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(errs, gc.HasLen, 1)
	c.Assert(errs[0], gc.ErrorMatches, `.* invalid loop volume ID "\.\./super/important/stuff"`)
}

func (s *loopSuite) TestDescribeVolumes(c *gc.C) {
	source, _ := s.loopVolumeSource(c)
	_, err := source.DescribeVolumes(context.Background(), []string{"a", "b"})
	c.Assert(err, jc.ErrorIs, errors.NotImplemented)
}

func (s *loopSuite) TestAttachVolumes(c *gc.C) {
	source, _ := s.loopVolumeSource(c)
	cmd := s.commands.expect("losetup", "-j", filepath.Join(s.storageDir, "volume-0"))
	cmd.respond("", nil) // no existing attachment
	cmd = s.commands.expect("losetup", "-f", "--show", filepath.Join(s.storageDir, "volume-0"))
	cmd.respond("/dev/loop98", nil) // first available loop device
	cmd = s.commands.expect("losetup", "-j", filepath.Join(s.storageDir, "volume-1"))
	cmd.respond("", nil) // no existing attachment
	cmd = s.commands.expect("losetup", "-f", "--show", "-r", filepath.Join(s.storageDir, "volume-1"))
	cmd.respond("/dev/loop99", nil)
	cmd = s.commands.expect("losetup", "-j", filepath.Join(s.storageDir, "volume-2"))
	cmd.respond("/dev/loop42: foo\n/dev/loop1: foo\n", nil) // existing attachments

	results, err := source.AttachVolumes(context.Background(), []storage.VolumeAttachmentParams{{
		Volume:   names.NewVolumeTag("0"),
		VolumeId: "vol-ume0",
		AttachmentParams: storage.AttachmentParams{
			Machine:    names.NewMachineTag("0"),
			InstanceId: "inst-ance",
		},
	}, {
		Volume:   names.NewVolumeTag("1"),
		VolumeId: "vol-ume1",
		AttachmentParams: storage.AttachmentParams{
			Machine:    names.NewMachineTag("0"),
			InstanceId: "inst-ance",
			ReadOnly:   true,
		},
	}, {
		Volume:   names.NewVolumeTag("2"),
		VolumeId: "vol-ume2",
		AttachmentParams: storage.AttachmentParams{
			Machine:    names.NewMachineTag("0"),
			InstanceId: "inst-ance",
		},
	}})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, []storage.AttachVolumesResult{{
		VolumeAttachment: &storage.VolumeAttachment{names.NewVolumeTag("0"),
			names.NewMachineTag("0"),
			storage.VolumeAttachmentInfo{
				DeviceName: "loop98",
			},
		},
	}, {
		VolumeAttachment: &storage.VolumeAttachment{names.NewVolumeTag("1"),
			names.NewMachineTag("0"),
			storage.VolumeAttachmentInfo{
				DeviceName: "loop99",
				ReadOnly:   true,
			},
		},
	}, {
		VolumeAttachment: &storage.VolumeAttachment{names.NewVolumeTag("2"),
			names.NewMachineTag("0"),
			storage.VolumeAttachmentInfo{
				DeviceName: "loop42",
			},
		},
	}})
}

func (s *loopSuite) TestDetachVolumes(c *gc.C) {
	source, _ := s.loopVolumeSource(c)
	fileName := filepath.Join(s.storageDir, "volume-0")
	cmd := s.commands.expect("losetup", "-j", fileName)
	cmd.respond("/dev/loop0: foo\n/dev/loop1: bar\n", nil)
	s.commands.expect("losetup", "-d", "/dev/loop0")
	s.commands.expect("losetup", "-d", "/dev/loop1")

	err := os.WriteFile(fileName, nil, 0644)
	c.Assert(err, jc.ErrorIsNil)

	errs, err := source.DetachVolumes(context.Background(), []storage.VolumeAttachmentParams{{
		Volume:   names.NewVolumeTag("0"),
		VolumeId: "vol-ume0",
		AttachmentParams: storage.AttachmentParams{
			Machine:    names.NewMachineTag("0"),
			InstanceId: "inst-ance",
		},
	}})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(errs, gc.HasLen, 1)
	c.Assert(errs[0], jc.ErrorIsNil)

	// file should not have been removed
	_, err = os.Stat(fileName)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *loopSuite) TestDetachVolumesDetachFails(c *gc.C) {
	source, _ := s.loopVolumeSource(c)
	fileName := filepath.Join(s.storageDir, "volume-0")
	cmd := s.commands.expect("losetup", "-j", fileName)
	cmd.respond("/dev/loop0: foo\n/dev/loop1: bar\n", nil)
	cmd = s.commands.expect("losetup", "-d", "/dev/loop0")
	cmd.respond("", errors.New("oy"))

	err := os.WriteFile(fileName, nil, 0644)
	c.Assert(err, jc.ErrorIsNil)

	errs, err := source.DetachVolumes(context.Background(), []storage.VolumeAttachmentParams{{
		Volume:   names.NewVolumeTag("0"),
		VolumeId: "vol-ume0",
		AttachmentParams: storage.AttachmentParams{
			Machine:    names.NewMachineTag("0"),
			InstanceId: "inst-ance",
		},
	}})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(errs, gc.HasLen, 1)
	c.Assert(errs[0], gc.ErrorMatches, `.* detaching loop device "loop0": oy`)

	// file should not have been removed
	_, err = os.Stat(fileName)
	c.Assert(err, jc.ErrorIsNil)
}
