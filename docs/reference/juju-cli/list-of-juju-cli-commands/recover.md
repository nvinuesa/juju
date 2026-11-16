(command-juju-recover)=
# `juju recover`
> See also: [bootstrap](#command-juju-bootstrap), [create-backup](#command-juju-create-backup), [unregister](#command-juju-unregister)

## Summary
Recover a controller from a backup archive.

## Usage
```text
juju recover [options] <backup-file> <assertion-file>
```

### Options
| Flag | Default | Usage |
| --- | --- | --- |
| `-B`, `--no-browser-login` | false | Do not use web browser for authentication |

## Details
Validate an archive against its unsigned SHA-256 assertion, provision a replacement controller, replay the archived databases and object store, and register the recovered controller. Recovery currently supports single-node LXD and MicroK8s controllers with file-backed object storage. The recovered controller keeps the source controller and model UUIDs and its workloads.
