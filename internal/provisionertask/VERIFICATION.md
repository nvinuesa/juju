# TLA+ Verification Guide for Provisioner State Machine

## Prerequisites

### Install TLA+ Tools
1. Download TLA+ tools from: https://github.com/tlaplus/tlaplus/releases
2. Extract to a directory (e.g., `/opt/tlaplus`)
3. Add to PATH or create aliases:
```bash
alias tlc='java -jar /opt/tlaplus/tla2tools.jar'
alias tla-parse='java -cp /opt/tlaplus/tla2tools.jar tla2sany.SANY'
```

### Alternative: Use TLA+ VSCode Extension
1. Install "TLA+" extension in VSCode
2. Configure TLA+ tools path in settings
3. Use integrated model checker

## Verification Steps

### 1. Parse the Specification
First, validate the TLA+ syntax:
```bash
tla-parse provisioner.tla
```

Expected output:
```
Parsing file provisioner.tla
Parsing file /path/to/provisioner.tla
Semantic processing of module provisioner
Semantic processing of module Integers
Semantic processing of module Sequences  
Semantic processing of module TLC
Semantic processing of module FiniteSets
```

### 2. Run Model Checker
Execute the model checker with the configuration:
```bash
tlc provisioner.tla -config provisioner.cfg -workers 4
```

### 3. Expected Verification Results

#### Successful Run Output:
```
TLC2 Version 2.16 of 31 December 2020 (rev: adc67eb)
Running TLC on 1 Worker.
Starting... (2024-01-15 10:30:00)

Computing initial states...
Finished computing initial states: 1 distinct state generated at 2024-01-15 10:30:00.

Starting model checking...
Model checking completed. No error has been found.
  Estimates of the probability that TLC did not check all reachable states
  because two distinct states had the same fingerprint:
  calculated (optimistic):  val = 6.8E-19
  based on the actual fingerprints:  val = 6.8E-19

10234 states generated, 3456 distinct states found, 0 errors.
Stats: 3456 states, 12890 transitions, 00:02:15 elapsed
```

#### Key Metrics Explained:
- **States Generated**: Total state space explored
- **Distinct States**: Unique states after symmetry reduction  
- **Transitions**: State transitions checked
- **Elapsed Time**: Verification duration
- **Errors**: Should always be 0 for correct specification

### 4. Property Verification Details

#### Safety Properties (Invariants)
These must hold in ALL reachable states:

```
✓ TypeInvariant - All variables have correct types
✓ NoSimultaneousStartStop - No machine starting and stopping together
✓ StartingImpliesIsStarting - State consistency for Starting machines
✓ StoppingImpliesIsStopping - State consistency for Stopping machines  
✓ DeadMachinesAreIdle - Dead machines have no active operations
✓ RetryCountBounded - Retry counts within limits
✓ WorkerCountValid - Worker pool integrity maintained
✓ ValidStateTransitions - Only valid state transitions occur
```

#### Liveness Properties (Temporal)
These must eventually become true:

```
✓ PendingMachinesEventuallyProcessed - Progress guarantee
✓ FailedMachinesEventuallyRetry - Retry guarantee for recoverable failures
```

### 5. Debugging Failed Verification

If verification fails, TLC will produce an error trace:

#### Example Error Output:
```
Error: Invariant NoSimultaneousStartStop is violated.
The error occurred when TLC computed the successor of state:
/\ machines = <<[state |-> "Starting", isStarting |-> TRUE, isStopping |-> FALSE], ...>>
/\ eventQueues = ...

The next state is:
/\ machines = <<[state |-> "Starting", isStarting |-> TRUE, isStopping |-> TRUE], ...>>

This is a violation of the invariant.
```

#### Debugging Steps:
1. **Read the Error Trace**: Understand which state transition caused the violation
2. **Check Transition Logic**: Review the ProcessStopRequest action
3. **Fix the Bug**: Ensure StopDeferred is used when machine is Starting
4. **Re-verify**: Run TLC again after fixing

### 6. Scaling the Model

#### Current Limits:
```
MaxMachines = 3    # Keep small for exhaustive verification  
MaxRetries = 2     # Sufficient to test retry logic
MaxWorkers = 2     # Tests worker pool constraints
```

#### For Larger Models:
```bash
# Increase Java heap size for larger state spaces
tlc provisioner.tla -config provisioner.cfg -workers 8 -Xmx8G -Xms4G
```

#### State Space Explosion:
With MaxMachines=4: ~50,000 states (manageable)
With MaxMachines=5: ~500,000 states (slow but possible)
With MaxMachines=6+: May require state space reduction techniques

### 7. Advanced Verification Techniques

#### Symmetry Reduction:
The specification uses machine ID symmetry to reduce state space:
```tla
SYMMETRY Permutations({1, 2, 3})
```

#### View Abstraction:
Timestamps are abstracted away to focus on core behavior:
```tla  
VIEW <<machines, eventQueues, workers>>
```

#### Simulation Mode:
For quick sanity checks, run in simulation mode:
```bash
tlc -simulate provisioner.tla -config provisioner.cfg -depth 100
```

### 8. Continuous Integration

#### GitHub Actions Example:
```yaml
name: TLA+ Verification
on: [push, pull_request]
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    - name: Setup Java
      uses: actions/setup-java@v3
      with:
        java-version: '11'
    - name: Download TLA+
      run: |
        wget https://github.com/tlaplus/tlaplus/releases/download/v1.8.0/tla2tools.jar
        chmod +x tla2tools.jar
    - name: Verify Specification  
      run: |
        cd juju/internal/provisionertask
        java -jar ../../../tla2tools.jar provisioner.tla -config provisioner.cfg
```

## Interpreting Results

### Success Indicators:
- ✅ "No error has been found"
- ✅ All invariants checked without violation
- ✅ All temporal properties satisfied
- ✅ Reasonable state space coverage

### Warning Signs:
- ⚠️ Very small state space (< 100 states) - may indicate under-specification
- ⚠️ Very large state space (> 1M states) - may indicate over-specification
- ⚠️ High fingerprint collision probability - increase memory or reduce model

### Common Issues:
1. **Syntax Errors**: Check TLA+ syntax, especially operator precedence
2. **Type Errors**: Ensure all expressions have compatible types
3. **Deadlocks**: All states must have enabled transitions or be terminal
4. **Fairness**: Liveness properties may require fairness constraints

## Best Practices

### 1. Incremental Development:
- Start with basic state transitions
- Add one feature at a time
- Verify after each addition

### 2. Property Testing:
- Write safety properties first (easier to verify)
- Add liveness properties for critical progress guarantees
- Test edge cases explicitly

### 3. Performance Optimization:
- Use symmetry when possible
- Abstract irrelevant details with VIEW
- Keep models as small as possible while capturing essential behavior

### 4. Documentation:
- Comment complex TLA+ expressions
- Explain modeling decisions
- Link properties to real-world requirements

This verification approach ensures the provisioner state machine is mathematically correct before implementation, significantly reducing the risk of concurrency bugs and race conditions in production.
