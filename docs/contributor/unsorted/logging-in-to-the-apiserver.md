(logging-in-to-the-apiserver)=
# Logging in to the API Server

This document is being written because every time I need to look at how
logging in to the API server works, I have to step through all the code and
remind myself. Once I have done that, everything seems obvious and not worth
writing down... until next time I need to think about the login process.

Intended work is to add the ability to have a user log in to the root of the
API server in order to get access to the model manager and user manager
facades. This allows the user to change their password, create models
and list the models that they are able to connect to.

The intended way that this is going to work is that the API client code will
attempt to login to the server using a versioned login command. There is
already a login-v1 that introduced new return values and a reauth request
field. We will build on that behaviour to add a login-v2. If and only if the
version 2 login is used the facades that are available at the apiserver will
be restricted.  Logging in using version 1 or the original will continue to
act as if you are logging into the controller model - the original one
and only model that would exist before the Juju Model Server work.

This work then needs to look at the API connection workflow from both ends,
the API client side, and the server side.

## apiserver.Server

apiserver.go

Server struct has a map of adminApiFactories. This currently has keys of
0 and 1.  We will need to add a 2 here.

The Server instance is created with NewServer, and listens on the API port.

The incoming socket connection is handled inside the (*Server).run method.
 - "/model/:modeluuid/api" -> srv.apiHandler
 - "/" -> srv.apiHandler

The apiHandler creates a go routine for handling the socket connection, and
extracts the modelUUID from the path, and calls the (*Server).serveConn method.

serveConn validates the model UUID and gets an appropiate *state.State
for the model specified.  An empty model UUID is treated as the
controller model here.  One thing that will need to change is passing
through whether or not we were given an model UUID, as this is the last
place that cares, but we will need to know later when the appropriate login
method is called. The various admin API instances are created here and passed
through to the newAnonRoot object.  This is passed in as the method finder for
the websocket connection.

root.go

newAnonRoot only responds to calls on the "Admin" rootName.  The version of
the FindMethod is used to determine whether the version 0 or 1 login API is
used.

The Login methods on the adminV0 and adminV1 instances call into the doLogin
method.  It is here where we want to wrap the authedApi with something that
limits the rootName objects that can be called.  In order to do this, we need
to pass the modelUUID from the path (remember this is either a valid uuid or the
empty string), and the version of the login command used.

In order to get the modelUUID from the path into the Login method, we need to
capture it in *Server.serveConn, where it is passed in as an argument.  We
should pass it through to the newApiHandler function and store it on the
apiHandler struct.  This handler is passed through as an arg to the
newAnonRoot, and to the factory methods for the admin APIs.


Implementation Notes:

The login process for both v0 and v1 logins are handled in the method
*admin.doLogin (in admin.go).  The method *Conn.ServeFinder on the *rpc.Conn
attribute of the apiHandler is initially given the anonymous root here:

```go
func (srv *Server) serveConn(wsConn *websocket.Conn, reqNotifier *requestNotifier, modelUUID string) error {
	// ...
		conn.ServeFinder(newAnonRoot(h, adminApis), serverError)
	// ...
```

If login is successful, the doLogin method changes out the method finder that
is used for the RPC connection just before the end of the return statement at
the end of the method.

```go
	// a *admin
	//
	// root is `*apiHander` from the admin struct.
	a.root.rpcConn.ServeFinder(authedApi, serverError)
```

In order to supply a restricted api at the root, the doLogin method will need
to take a version number.  Just before we replace the method finder for the
RPC connection, we check the version number and the modelUUID of the api
handler, and wrap the authedApi in a restricted root method finder implemented
in a similar manner to the upgradingRoot method finder.

