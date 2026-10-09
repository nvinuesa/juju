(command-juju-recovery)=
# `juju recovery`
> See also: [bootstrap](#command-juju-bootstrap), [create-backup](#command-juju-create-backup), [unregister](#command-juju-unregister)

## Summary
Recover a controller from a backup archive.

## Usage
```text
juju recovery [options] <backup-file>
```

### Options
| Flag | Default | Usage |
| --- | --- | --- |
| `-B`, `--no-browser-login` | false | Do not use web browser for authentication |
| `--sha256` |  | SHA-256 checksum of the backup archive (64 hexadecimal characters) |

## Examples
    juju recovery juju-backup.tar.gz --sha256 <checksum>

## Details
Recover a replacement controller using the cloud, region and controller name
recorded in the archive. The controller UUID, model UUIDs and controller CA are
preserved. Recovery requires the same Juju agent version as the backup.

Fence the source controller before recovery. If its name is registered on this
client, run juju unregister first. Kubernetes recovery requires the original
cluster and surviving workload namespaces.

The archive checksum is required. Local credentials are tried first; if none
are available, the archived controller model credential is used. An ambiguous
credential selection or authentication failure stops recovery.