(controller-recovery)=
# Controller recovery

Controller recovery creates a replacement host and restores the source's
logical identity before normal controller workers start. The controller UUID,
model UUIDs, CA and workload records survive; the replacement's instance ID,
addresses, database membership and local authentication are rebuilt.

## Provisioning and startup

The client first verifies the archive and derives a provisioning plan. On
machines, the plan uses the archived agent, source base, constraints, hardware
capacity and provider configuration. The replacement receives a unique physical
instance name while retaining the source's logical machine name. Disk capacity
includes the archive, database import workspace and additional headroom.

On Kubernetes, the backup records the source pod's controller image references,
architecture, volume capacity and storage class, and Service exposure. Recovery
reuses those references; eligible nodes must be able to pull the images or have
them preloaded. Tags can change, so retain the original image or use a digest
for immutable selection. The running agent must match the exact archived
version, including its build number. Workload models are checked against their
own clusters and credentials. Only the hosting cluster is checked for controller
fencing.

The client provisions the replacement, transfers the archive and starts its
agent with recovery selected. The agent initialises Dqlite, imports the databases
and objects, patches physical facts, and retires lost controller peers. The
bootstrap and recovery workers have complementary startup flags. The inactive
worker cannot gather the other operation's dependencies or run its setup.

After import completes, the ordinary workers start against the recovered state.
The client verifies the replacement API and saves its connection information,
including a Kubernetes proxy when needed. Normal reconciliation resumes,
including operations that were pending when the backup was taken.

## Bootstrap comparison

| Decision | Fresh bootstrap | Recovery |
| --- | --- | --- |
| Identity | New controller, models and CA | Archived identities and CA |
| Machine agent and base | Bootstrap selection and defaults | Exact archived agent and recorded base |
| Kubernetes images | Selected image references | Source pod image references |
| Capacity | Requested constraints and shared defaults | Source requirements and observed capacity, grown for import |
| Initial database state | Create initial domain records | Import and patch archived domain records |
| Worker selection | Bootstrap enabled, recovery disabled | Recovery enabled, bootstrap disabled |
| Failed provisioning | Provider bootstrap cleanup unless `--keep-broken` | Cleanup only resources created by this attempt unless `--keep-broken` |

## Restored storage and topology

Recovery produces one controller node. Other controller units and dedicated
machines are retired in the restored records without deleting their cloud
resources. A controller host containing unrelated workload units or containers
is refused before provisioning because retiring that host would affect them.

Objects are restored into local file storage. The archived S3 object store
configuration does not survive import: recovery forces the controller
configuration's object store type to file and removes the archived S3
settings, so a recovered controller never reattaches the source's bucket on
its own. Returning to S3 is an explicit operator decision afterwards, with
fresh credentials; the usual file-to-S3 transition then uploads the restored
objects.

For the operator procedure, see {ref}`manage-controllers`.
