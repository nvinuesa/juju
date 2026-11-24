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
Controller recovery uses a backup archive and its unsigned SHA-256 assertion
to restore a controller on a replacement machine or Kubernetes namespace.
The client must be able to resolve the cloud named in the archive. On a new
workstation, add the cloud with 'juju add-cloud --client' if it is not
already available.

On a machine, recovery uploads and verifies the archive, then initialises the
replacement Dqlite database with its controller-node row. On Kubernetes, it
installs and starts the replacement agent without initialising the database.
Neither path imports archived state or registers the controller. The command
leaves the replacement for inspection and exits with an error reporting its
instance ID or namespace.