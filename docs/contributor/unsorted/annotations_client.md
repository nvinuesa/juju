(annotations_client)=
# Annotations client

New annotations client is available from 1.22 effectively deprecating
annotations in old client.

This client provides functionality to annotate charms in addition
to model, machine, service and unit previously done
through our old client.

New annotations client also supports bulk calls.

## API

Note that where SET call returns an error, Error in GET call return is params.ErrorResult.

### SET
For the SET annotations call that looks similar to this:

    ......{
            "Type": "Annotations",
            "Request": "Set",
            "Params": {
                 "Annotations": {{
                    "EntityTag": a, "Annotations": pairs1
                  },{
                    "EntityTag": b, "Annotations": pairs2
                  }}
    }}......
### GET
Corresponding GET annotations call may look like:

    ......{
            "Type": "Annotations",
            "Request": "Get",
            "Params": {
                 "Entities": {
                     {Entity {"Tag": a}},
                     {Entity {"Tag": b},
                     }
    }}......

Returning

    {
     "Results": {
          {"EntityTag": a, "Annotations": pairs1, "Error": nil},
          {"EntityTag": b, "Annotations": pairs2, "Error": nil},

    }}
