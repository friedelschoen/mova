# mova

*mova* is a small domain-specific language (DSL) for defining **state machines**
with **states**, **triggers**, and **actions**.
It is designed to be declarative, readable, and extensible, and serves as a simple
way to express event-driven behavior in a static configuration file.

## Overview

A *mova* file (`.mova`) describes one or more **states**, each with optional
**initial actions** and **triggers**.
A trigger listens for an event and can execute actions or move to another state.

The language supports:
- **constants** via `key = value;`
- **states** via `state name { ... };`
- **actions** (function calls with arguments)
- **state transitions** via `move <state>`
- **event-data** via `on EVENT(x=..., y=...) -> ACTIONS;`

## Syntax

### 1. Constants

```
press = 1;
release = 0;
```

Constants are **variables** and can later be used as action arguments or event-data.
Supported types: integers, floats, strings, booleans.


### 2. States

Each state defines optional init actions and one or more triggers.

```
state init {
    set_led(led1=1, led2=0, led3=0, led4=0);
    on A(event=press) -> KEY_A, KEY_B;
    on B(event=press) -> move cursor;
};
```

* `state init { ... };` defines a named state.
* The **init** section (before any `on`) runs when entering the state.
* `on EVENT(...) -> ...;` defines a trigger and its reactions.


### 3. Triggers

Triggers react to incoming events.
Each trigger may define one or more *event-data* fields that must match the trigger’s spec.

```
on ACCEL(x, y, z) -> MOUSE_MOVE(x, y, z);
```

* Left side (`x, y, z`) = event-data received from an `Emit()` call.
* Right side = one or more actions, separated by commas.
* Each action can have **arguments**.


### 4. Actions

Actions refer to functions defined in the registry (e.g. `KEY_A`, `MOUSE_MOVE`, etc.).
Arguments are evaluated as **variables** (constants or event-data).

```
MOUSE_MOVE(x=x, y=y, z=z);
set_led(led1=1, led2=0, led3=0, led4=0);
```


### 5. State Transitions

A transition moves execution to another state:

```
move cursor;
```

When executed, `cursor` becomes the active state.
Its init actions will run automatically.


## Full Example

```
# Wiimote mapping example

press   = 1;
release = 0;

state init {
    set_led(led1=1, led2=0, led3=0, led4=0);
    on A(event=press)   -> KEY_A, KEY_B;
    on B(event=press)   -> move cursor;
};

state cursor {
    set_led(led1=0, led2=1, led3=0, led4=0);
    on ACCEL(x, y, z)   -> MOUSE_MOVE(x, y, z);
    on A(event=press)   -> MOUSE_LEFT(event=press);
    on B(event=press)   -> move init;
};
```


## Type Checking and Error Messages

*mova* enforces type consistency between constants, event-data, and actions.

| Category           | Example message                                                               |
| ------------------ | ----------------------------------------------------------------------------- |
| Unknown trigger    | `unspecified trigger "XYZ"`                                                   |
| Unknown action     | `unspecified action "SET_LED"`                                                |
| Missing event-data | `unspecified event-data "x" for trigger ACCEL`                                |
| Type mismatch      | `type mismatch for argument MOUSE_MOVE.x: expected ValueInt, got ValueString` |
| Undefined variable | `undefined variable "foo"`                                                    |

Terminology is consistent across all errors:

* **unspecified** → not declared in the spec
* **event-data** → arguments passed to `Emit`
* **argument** → parameters of an action call
* **variable** → either a constant or event-data


## Interpreter Architecture

The reference implementation consists of:

| File              | Purpose                                           |
| ----------------- | ------------------------------------------------- |
| `lexer.go`        | Tokenizes source text                             |
| `parser.go`       | Builds an AST from tokens                         |
| `statemachine.go` | Compiles the AST into an executable state machine |
| `main.go`         | Example usage with a Wiimote registry             |


## File Extension

`.mova`
(Alternative suggestions: `.mach`, `.trig` — depending on final project name.)


## Design Goals

* Minimal syntax, easy to read and parse
* Consistent, precise error reporting
* Embeddable in Go programs
* Plan 9-inspired simplicity — no runtime magic, just parsing and execution


## License

Zlib