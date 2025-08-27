-------------------------------- MODULE provisioner --------------------------------
\* TLA+ specification for the Juju provisioner state machine
\* This models the state transitions and verifies key safety properties

EXTENDS Integers, Sequences, TLC, FiniteSets

CONSTANTS
    MaxMachines,     \* Maximum number of machines to model
    MaxRetries,      \* Maximum number of retry attempts
    MaxWorkers       \* Maximum number of concurrent workers

VARIABLES
    machines,        \* Map from machine ID to machine state record
    eventQueues,     \* The three input queues: start, stop, retry, response
    workers,         \* Available worker slots (semaphore)
    nextMachineId    \* Counter for generating unique machine IDs

\* Machine states
MachineStates == {"Pending", "Starting", "Running", "Stopping", "StopDeferred", "Failed", "Dead"}

\* Events that can be sent to machines
Events == {"StartRequested", "StartSucceeded", "StartFailed", "StopRequested", "StopCompleted", "RetryRequested"}

\* Queue types
QueueTypes == {"start", "stop", "retry", "response"}

\* Machine record structure
MachineRecord == [
    state: MachineStates,
    retryCount: 0..MaxRetries,
    isStarting: BOOLEAN,
    isStopping: BOOLEAN
]

\* Event message structure  
EventMessage == [
    machineId: 1..MaxMachines,
    event: Events,
    timestamp: Nat
]

\* Type invariants
TypeInvariant ==
    /\ machines \in [1..MaxMachines -> MachineRecord \cup {NULL}]
    /\ eventQueues \in [QueueTypes -> Seq(EventMessage)]
    /\ workers \in 0..MaxWorkers
    /\ nextMachineId \in 1..(MaxMachines + 1)

\* Initial state
Init ==
    /\ machines = [i \in 1..MaxMachines |-> NULL]
    /\ eventQueues = [q \in QueueTypes |-> <<>>]
    /\ workers = MaxWorkers
    /\ nextMachineId = 1

\* Helper functions
MachineExists(id) == machines[id] # NULL

GetMachine(id) == machines[id]

SetMachine(id, machine) == machines' = [machines EXCEPT ![id] = machine]

\* Valid state transitions based on the state machine design
ValidTransition(currentState, event, nextState) ==
    \/ /\ currentState = "Pending" /\ event = "StartRequested" /\ nextState = "Starting"
    \/ /\ currentState = "Starting" /\ event = "StartSucceeded" /\ nextState = "Running"  
    \/ /\ currentState = "Starting" /\ event = "StartFailed" /\ nextState = "Failed"
    \/ /\ currentState = "Starting" /\ event = "StopRequested" /\ nextState = "StopDeferred"
    \/ /\ currentState = "Running" /\ event = "StopRequested" /\ nextState = "Stopping"
    \/ /\ currentState = "Stopping" /\ event = "StopCompleted" /\ nextState = "Dead"
    \/ /\ currentState = "StopDeferred" /\ event = "StartSucceeded" /\ nextState = "Stopping"
    \/ /\ currentState = "StopDeferred" /\ event = "StartFailed" /\ nextState = "Dead"
    \/ /\ currentState = "Failed" /\ event = "RetryRequested" /\ nextState = "Pending"
    \/ /\ currentState = "Failed" /\ event = "StopRequested" /\ nextState = "Dead"

\* Enqueue an event to the appropriate queue
EnqueueEvent(queueType, event) ==
    eventQueues' = [eventQueues EXCEPT ![queueType] = Append(@, event)]

\* Dequeue an event from a queue
DequeueEvent(queueType) ==
    /\ Len(eventQueues[queueType]) > 0
    /\ eventQueues' = [eventQueues EXCEPT ![queueType] = Tail(@)]
    /\ Head(eventQueues[queueType])

\* Actions for different events

\* Create a new machine when StartRequested arrives for non-existent machine
CreateNewMachine ==
    /\ nextMachineId <= MaxMachines
    /\ \E event \in DOMAIN eventQueues["start"]:
        /\ Len(eventQueues["start"]) > 0
        /\ LET msg == Head(eventQueues["start"])
           IN /\ msg.event = "StartRequested"
              /\ ~MachineExists(msg.machineId)
              /\ machines' = [machines EXCEPT ![msg.machineId] = [
                    state |-> "Pending",
                    retryCount |-> 0,
                    isStarting |-> FALSE,
                    isStopping |-> FALSE
                 ]]
              /\ eventQueues' = [eventQueues EXCEPT !["start"] = Tail(@)]
              /\ nextMachineId' = nextMachineId + 1
              /\ UNCHANGED workers

\* Process a start event that transitions to Starting state
ProcessStartEvent ==
    /\ Len(eventQueues["start"]) > 0
    /\ workers > 0  \* Need available worker
    /\ LET msg == Head(eventQueues["start"])
       IN /\ msg.event = "StartRequested"
          /\ MachineExists(msg.machineId)
          /\ LET machine == GetMachine(msg.machineId)
             IN /\ ValidTransition(machine.state, msg.event, "Starting")
                /\ machines' = [machines EXCEPT ![msg.machineId] = [
                      state |-> "Starting",
                      retryCount |-> machine.retryCount,
                      isStarting |-> TRUE,
                      isStopping |-> machine.isStopping
                   ]]
                /\ eventQueues' = [eventQueues EXCEPT !["start"] = Tail(@)]
                /\ workers' = workers - 1  \* Acquire worker
                /\ UNCHANGED nextMachineId

\* Process start success response
ProcessStartSuccess ==
    /\ Len(eventQueues["response"]) > 0
    /\ LET msg == Head(eventQueues["response"])
       IN /\ msg.event = "StartSucceeded"
          /\ MachineExists(msg.machineId)
          /\ LET machine == GetMachine(msg.machineId)
             IN /\ ValidTransition(machine.state, msg.event, "Running")
                /\ machines' = [machines EXCEPT ![msg.machineId] = [
                      state |-> "Running",
                      retryCount |-> machine.retryCount,
                      isStarting |-> FALSE,
                      isStopping |-> machine.isStopping
                   ]]
                /\ eventQueues' = [eventQueues EXCEPT !["response"] = Tail(@)]
                /\ workers' = workers + 1  \* Release worker
                /\ UNCHANGED nextMachineId

\* Process start failure response  
ProcessStartFailure ==
    /\ Len(eventQueues["response"]) > 0
    /\ LET msg == Head(eventQueues["response"])
       IN /\ msg.event = "StartFailed"
          /\ MachineExists(msg.machineId)
          /\ LET machine == GetMachine(msg.machineId)
             IN /\ ValidTransition(machine.state, msg.event, "Failed")
                /\ machines' = [machines EXCEPT ![msg.machineId] = [
                      state |-> "Failed", 
                      retryCount |-> machine.retryCount + 1,
                      isStarting |-> FALSE,
                      isStopping |-> machine.isStopping
                   ]]
                /\ eventQueues' = [eventQueues EXCEPT !["response"] = Tail(@)]
                /\ workers' = workers + 1  \* Release worker
                /\ UNCHANGED nextMachineId

\* Process stop request
ProcessStopRequest ==
    /\ Len(eventQueues["stop"]) > 0
    /\ LET msg == Head(eventQueues["stop"])
       IN /\ msg.event = "StopRequested"
          /\ MachineExists(msg.machineId)
          /\ LET machine == GetMachine(msg.machineId)
             IN \/ /\ ValidTransition(machine.state, msg.event, "Stopping")
                   /\ workers > 0  \* Need worker for stop operation
                   /\ machines' = [machines EXCEPT ![msg.machineId] = [
                         state |-> "Stopping",
                         retryCount |-> machine.retryCount,
                         isStarting |-> machine.isStarting,
                         isStopping |-> TRUE
                      ]]
                   /\ workers' = workers - 1
                \/ /\ ValidTransition(machine.state, msg.event, "StopDeferred")
                   /\ machines' = [machines EXCEPT ![msg.machineId] = [
                         state |-> "StopDeferred",
                         retryCount |-> machine.retryCount,
                         isStarting |-> machine.isStarting,
                         isStopping |-> TRUE
                      ]]
                   /\ UNCHANGED workers
                \/ /\ ValidTransition(machine.state, msg.event, "Dead")
                   /\ machines' = [machines EXCEPT ![msg.machineId] = [
                         state |-> "Dead",
                         retryCount |-> machine.retryCount,
                         isStarting |-> FALSE,
                         isStopping |-> FALSE
                      ]]
                   /\ UNCHANGED workers
          /\ eventQueues' = [eventQueues EXCEPT !["stop"] = Tail(@)]
          /\ UNCHANGED nextMachineId

\* Process stop completion
ProcessStopCompletion ==
    /\ Len(eventQueues["response"]) > 0
    /\ LET msg == Head(eventQueues["response"])
       IN /\ msg.event = "StopCompleted"
          /\ MachineExists(msg.machineId)
          /\ LET machine == GetMachine(msg.machineId)
             IN /\ ValidTransition(machine.state, msg.event, "Dead")
                /\ machines' = [machines EXCEPT ![msg.machineId] = [
                      state |-> "Dead",
                      retryCount |-> machine.retryCount,
                      isStarting |-> FALSE,
                      isStopping |-> FALSE
                   ]]
                /\ eventQueues' = [eventQueues EXCEPT !["response"] = Tail(@)]
                /\ workers' = workers + 1  \* Release worker
                /\ UNCHANGED nextMachineId

\* Process retry request
ProcessRetryRequest ==
    /\ Len(eventQueues["retry"]) > 0
    /\ LET msg == Head(eventQueues["retry"])
       IN /\ msg.event = "RetryRequested"
          /\ MachineExists(msg.machineId)
          /\ LET machine == GetMachine(msg.machineId)
             IN /\ ValidTransition(machine.state, msg.event, "Pending")
                /\ machine.retryCount < MaxRetries  \* Don't exceed retry limit
                /\ machines' = [machines EXCEPT ![msg.machineId] = [
                      state |-> "Pending",
                      retryCount |-> machine.retryCount,
                      isStarting |-> FALSE,
                      isStopping |-> machine.isStopping
                   ]]
                /\ eventQueues' = [eventQueues EXCEPT !["retry"] = Tail(@)]
                /\ UNCHANGED <<workers, nextMachineId>>

\* Simulate external events (new machine requests, async operation completions)
SimulateStartRequest ==
    /\ nextMachineId <= MaxMachines
    /\ EnqueueEvent("start", [
          machineId |-> nextMachineId,
          event |-> "StartRequested", 
          timestamp |-> nextMachineId
       ])
    /\ UNCHANGED <<machines, workers, nextMachineId>>

SimulateAsyncStartCompletion ==
    /\ \E id \in 1..MaxMachines:
          /\ MachineExists(id)
          /\ GetMachine(id).state = "Starting"
          /\ GetMachine(id).isStarting
          /\ \/ EnqueueEvent("response", [
                   machineId |-> id,
                   event |-> "StartSucceeded",
                   timestamp |-> id
                ])
             \/ EnqueueEvent("response", [
                   machineId |-> id,
                   event |-> "StartFailed", 
                   timestamp |-> id
                ])
    /\ UNCHANGED <<machines, workers, nextMachineId>>

SimulateAsyncStopCompletion ==
    /\ \E id \in 1..MaxMachines:
          /\ MachineExists(id)
          /\ GetMachine(id).state = "Stopping"
          /\ GetMachine(id).isStopping
          /\ EnqueueEvent("response", [
                machineId |-> id,
                event |-> "StopCompleted",
                timestamp |-> id
             ])
    /\ UNCHANGED <<machines, workers, nextMachineId>>

\* Next state relation
Next ==
    \/ CreateNewMachine
    \/ ProcessStartEvent
    \/ ProcessStartSuccess
    \/ ProcessStartFailure
    \/ ProcessStopRequest
    \/ ProcessStopCompletion
    \/ ProcessRetryRequest
    \/ SimulateStartRequest
    \/ SimulateAsyncStartCompletion
    \/ SimulateAsyncStopCompletion

\* Complete specification
Spec == Init /\ [][Next]_<<machines, eventQueues, workers, nextMachineId>>

\* SAFETY PROPERTIES TO VERIFY

\* No machine can be both starting and stopping simultaneously
NoSimultaneousStartStop ==
    \A id \in 1..MaxMachines:
        MachineExists(id) =>
            ~(GetMachine(id).isStarting /\ GetMachine(id).isStopping)

\* Machines in Starting state must have isStarting = TRUE
StartingImpliesIsStarting ==
    \A id \in 1..MaxMachines:
        MachineExists(id) =>
            (GetMachine(id).state = "Starting") <=> GetMachine(id).isStarting

\* Machines in Stopping state must have isStopping = TRUE  
StoppingImpliesIsStopping ==
    \A id \in 1..MaxMachines:
        MachineExists(id) =>
            (GetMachine(id).state = "Stopping") <=> GetMachine(id).isStopping

\* Dead machines are no longer starting or stopping
DeadMachinesAreIdle ==
    \A id \in 1..MaxMachines:
        MachineExists(id) =>
            (GetMachine(id).state = "Dead") =>
                (~GetMachine(id).isStarting /\ ~GetMachine(id).isStopping)

\* Retry count never exceeds maximum
RetryCountBounded ==
    \A id \in 1..MaxMachines:
        MachineExists(id) => GetMachine(id).retryCount <= MaxRetries

\* Worker count is never negative and never exceeds maximum
WorkerCountValid == 
    /\ workers >= 0
    /\ workers <= MaxWorkers

\* No machine can transition to an invalid state
ValidStateTransitions ==
    \A id \in 1..MaxMachines:
        MachineExists(id) => GetMachine(id).state \in MachineStates

\* LIVENESS PROPERTIES

\* Every pending machine will eventually be processed (assuming sufficient workers)
\* This requires fairness assumptions about event processing
PendingMachinesEventuallyProcessed ==
    \A id \in 1..MaxMachines:
        MachineExists(id) =>
            (GetMachine(id).state = "Pending") ~> 
            (GetMachine(id).state # "Pending")

\* Failed machines with retries left will eventually retry
FailedMachinesEventuallyRetry ==
    \A id \in 1..MaxMachines:
        MachineExists(id) =>
            (GetMachine(id).state = "Failed" /\ GetMachine(id).retryCount < MaxRetries) ~>
            (GetMachine(id).state = "Pending")

\* INVARIANTS (always true)
Invariants ==
    /\ TypeInvariant
    /\ NoSimultaneousStartStop
    /\ StartingImpliesIsStarting
    /\ StoppingImpliesIsStopping  
    /\ DeadMachinesAreIdle
    /\ RetryCountBounded
    /\ WorkerCountValid
    /\ ValidStateTransitions

====
