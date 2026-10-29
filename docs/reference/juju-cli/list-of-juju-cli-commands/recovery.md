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
| `--keep-broken` | false | Do not remove replacement cloud resources if recovery fails |
| `--sha256` |  | SHA-256 checksum of the backup archive (64 hexadecimal characters) |

## Examples
    juju recovery juju-backup.tar.gz --sha256 <checksum>

## Details
Recover a replacement controller using the cloud, region and controller name
recorded in the archive. The controller UUID, model UUIDs and controller CA are
preserved. The replacement runs the exact agent version recorded in the backup.
Machine agents come from the archive; the CLI version does not select them.
Source constraints and observed hardware determine the replacement's capacity,
with additional space reserved for archive staging and database import.

Kubernetes backups record the source agent and controller charm image references,
platform, volume size, storage class and Service exposure. Recovery reuses those
references, which may be tags or digests. Images must be available to eligible
cluster nodes through configured pulls or preloading. The running agent must have
the exact archived version, including its build number.

Fence the source controller before recovery. If its name is registered on this
client, run juju unregister first. Kubernetes recovery requires the original
cluster and surviving workload namespaces. Each workload model is checked on
its own archived cluster before the replacement is created.

The archive checksum is required. Local credentials are tried first; if none
are available, the credential archived for each model cloud is used. An ambiguous
credential selection or authentication failure stops recovery.

By default, a failed recovery removes the replacement's cloud resources.
Use --keep-broken to retain them for diagnosis and manual repair. The failed
controller is removed from the local client; access the retained resources
directly using SSH or your cloud tools. Stop recovery and normal controller
processes before starting safe mode. This flag does not pause recovery or make
an incomplete import resumable. Remove retained resources manually when finished.

Recovery produces one controller node. Lost controller peers are retired in the
restored state without deleting their provider resources. Controller machines
hosting other workload units or containers are refused before provisioning.
The restored object store remains file-backed until the operator explicitly
authorises an S3 transition with authorize-s3-transition=true.