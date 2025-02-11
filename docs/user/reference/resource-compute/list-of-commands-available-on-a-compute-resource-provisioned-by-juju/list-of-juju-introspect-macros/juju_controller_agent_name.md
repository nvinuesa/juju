(juju_controller_agent_name)=
# `juju_controller_agent_name`

The `juju_controller_agent_name` macro returns the name of the controller 
agent that is currently running.

## Usage

```python
juju_controller_agent_name
```

Returns a string containing the agent (controller) name.

_Note: This macro is only available on controller machines, everywhere else
it returns an empty string._

## Example output:

```bash
controller-0
``` 
