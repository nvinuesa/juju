(system-ssh-key)=
# System SSH Key
The idea of having a system SSH key is to support a number of real and
potential use-cases.
 * The system ssh key could be used to monitor the bootstrap process, and this
   would benefit the new users that don't have an existing SSH key
 * Allows the api server machines to ssh to other machines in the model
   * could be used to set up ssh tunnels through a single public facing IP
     address on the server
   * allows juju-exec commands to be run on remote machiens

Juju already creates a private key for serving the mongo database. It was an
option to also use this key, but in the end, having different keys for
different purposes just seems like a more robust idea.

A system key is generated when the model is bootstrapped, and uploaded
as part of the cloud-init machine creation process. The public key part is
added to the authorized keys list.

This means that we need to generate an identity file and the authorized key
line prior to creating the new machine.

If subsequent controller machines are created, they also need to have the
system identity file on them. Actually, it is most likely the API server jobs
that we really care about.

