// Package configfilter implements a Skipper data client that receives route configuration through a filter
// in a Skipper route.
//
// The route containing the config filter can be defined in the initialization options of the data client (see
// SelfRoutes), or in routes taken from another data client. The data client needs to be passed to Skipper among
// the custom data clients.
//
// The config filter provides an HTTP API to get/set/delete all or individual routes. The exact endpoints. For
// individual routes, :routeid wildcard.
//
// See the value of the APIDescription constant for the API description.
package configfilter

// APIDescription is printed when help content is requested.
const APIDescription = `
# Skipper routing table API

The paths to the config routes can be reconfigured by custom Skipper routing. This help reflects the default
settings where the root path for the config is /__config and the individual routes can be accessed at
/__config/<routeid>.

In all requests, changes to the default routes that the config filter was initialized with, typically containing
the routes with the config filter itself, are ignored.

### Root - All routes

Path: /__config

OPTIONS: returns this document
HEAD: returns the header of the responses sent to the GET request

GET:

Get all route definitions maintined by the configfilter data client in eskip format. If the query parameter
?pretty=false is set, pretty printing is omitted.

PUT and POST:

Set the complete routing table. Expects route definitions in eskip format, as text/plain or application/eskip.
Routes missing form the request document and existing in the current routing table will be deleted.

PATCH: Upsert routes in the routing table. It is like PUT or POST but not deleting existing routes.

DELETE:

Deletes routes by ID found in the request payload. Accepts eskip documents with content type text/plain or
application/eskip, where only the ID is used, or it accepts a comma separated list of IDs. IDs that are not
found in the current routing table are ignored. Routes in the default configuration of the filter are not
deleted.

### Individual routes

Path: /__config/<routeid>

OPTIONS: returns this document
HEAD: returns the header of the responses sent to the GET request

GET:

Returns the route as a route expression with ID=<routeid>, without the ID. If the query parameter ?pretty=false
is set, pretty printing is omitted.

PUT and POST:

Set the route with ID=<routeid>. Expects a single route expression in eskip format. If the payload contains a
route ID, it is ignored, and the ID derived from the path is used. If the route doesn't exist, it gets inserted,
if it exists, it gets updated.

PATCH: Updates a route if it exists. 
DELETE: Deletes a route if it exists.
`
