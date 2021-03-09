package fsm

import (
	"fmt"
	"time"
)

const QueueInfoKey string = "queue_info"

// timeout is only for when a state is triggered
// Set intentionally to var so it can be overridden
var InputTimeout = 15 * time.Minute

// GetStateMap converts a StateMachine into a StateMap
func GetStateMap(stateMachine StateMachine) StateMap {
	stateMap := make(StateMap, 0)
	for _, buildState := range stateMachine {
		stateMap[buildState(nil, nil).Slug] = buildState
	}
	return stateMap
}

func TriggerState(platform, uuid, targetState string, input interface{}, InputTransformer InputTransformer, store Store, emitter Emitter, stateMap StateMap) error {
	// Get Traverser
	traverser, _, err := getTraverser(platform, uuid, store)
	if err != nil {
		return fmt.Errorf("traverser with id (%s) not found, %w", uuid, err)
	}

	// check that the current state is exitable
	// past this block we can assume current state is exitable
	curState, err := traverser.CurrentState()
	if err != nil {
		return fmt.Errorf("failed to get current state from traverser, %w", err)
	}
	canExit, ok := checkStateExitable(curState, stateMap)
	if !ok {
		return fmt.Errorf("state (%s) does not exist", curState)
	}

	// if they cant exit their current state then queue it
	if !canExit {
		err = traverser.AddQueuedState(targetState, input)
		if err != nil {
			return fmt.Errorf("failed to enqueue state, %w", err)
		}

		// cant go any further
		return nil
	}

	lastUpdate, err := traverser.GetLastUpdateTime()
	if err != nil {
		return fmt.Errorf("failed to get last update time, %w", err)
	}

	// check if lastUpdate was even set
	if !lastUpdate.IsZero() {
		// check if its past the timeout
		if !time.Now().UTC().After(lastUpdate.Add(InputTimeout)) {
			// we have to queue it because another state is already in progress
			err = traverser.AddQueuedState(targetState, input)
			if err != nil {
				return fmt.Errorf("failed to enqueue state, %w", err)
			}
			// we cant go any further
			return nil
		}
	}

	// we can actually handle the state now
	stateObj, ok := stateMap[targetState]
	if !ok {
		return fmt.Errorf("state (%s) does not exist", targetState)
	}

	// set the current state in the traverser
	err = traverser.SetCurrentState(targetState)
	if err != nil {
		return fmt.Errorf("failed to set target state, %w", err)
	}

	err = traverser.SetLastUpdateTime(time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to set last updated time, %w", err)
	}

	// set info key
	err = traverser.Upsert(QueueInfoKey, input)
	if err != nil {
		return fmt.Errorf("failed to upsert queue info, %w", err)
	}

	// now that we know that's a valid state we can set it in the traverser
	currentState := stateObj(emitter, traverser)
	err = performEntryAction(currentState, emitter, traverser, stateMap)
	if err != nil {
		return fmt.Errorf("failed to perform entry action triggered state, %w", err)
	}

	return nil
}

func checkStateExitable(state string, stateMap StateMap) (isExitable bool, ok bool) {
	stateObj, ok := stateMap[state]
	if !ok {
		return false, false
	}

	return stateObj(nil, nil).IsExitable, true
}

// Step performs a single step through a StateMachine.
//
// This function handles the nuance of the logic for a single step through a state machine.
// ALL fsm-target's should call Step directly, and not attempt to handle the process of stepping through
// the StateMachine, so all platforms function with the same logic.
func Step(platform, uuid string, input interface{}, InputTransformer InputTransformer, store Store, emitter Emitter, stateMap StateMap) error {
	// Get Traverser
	traverser, newTraverser, err := getTraverser(platform, uuid, store)
	if err != nil {
		return fmt.Errorf("traverser with id (%s) not found, %w", uuid, err)
	}

	// Get current state
	traverserCurState, err := traverser.CurrentState()
	if err != nil {
		return fmt.Errorf("failed to get current state from traverser %w", err)
	}

	stateObj, ok := stateMap[traverserCurState]
	if !ok {
		return fmt.Errorf("state (%s) does not exist", traverserCurState)
	}

	currentState := stateObj(emitter, traverser)
	if newTraverser {
		err = performEntryAction(currentState, emitter, traverser, stateMap)
		if err != nil {
			return fmt.Errorf("failed to perform action entry, %w", err)
		}
	}

	// Transition
	intent, params := InputTransformer(input, currentState.ValidIntents())
	if intent != nil {
		newState := currentState.Transition(intent, params)
		if newState != nil {
			err = traverser.SetCurrentState(newState.Slug)
			if err != nil {
				return fmt.Errorf("failed to set current state during transition, %w", err)
			}
			err = traverser.SetLastUpdateTime(time.Now().UTC())
			if err != nil {
				return fmt.Errorf("failed to set last update time, %w", err)
			}
			err = performEntryAction(newState, emitter, traverser, stateMap)
			if err != nil {
				return fmt.Errorf("failed to perform action entry during transition, %w", err)
			}
		} else {
			err = currentState.Entry(true)
			if err != nil {
				return fmt.Errorf("failed to enter current state, %w", err)
			}
		}
	} else {
		err = currentState.Entry(true)
		if err != nil {
			return fmt.Errorf("failed to enter current state, %w", err)
		}
	}

	return nil
}

func getTraverser(platform, uuid string, store Store) (Traverser, bool, error) {
	newTraverser := false
	traverser, err := store.FetchTraverser(uuid)
	if err != nil {
		traverser, err = store.CreateTraverser(uuid)
		if err != nil {
			return nil, false, fmt.Errorf("failed to create traverser for id (%s), %w", uuid, err)
		}

		err = traverser.SetCurrentState(StartState)
		if err != nil {
			return nil, false, fmt.Errorf("failed to set current state to start state %w", err)
		}

		err = traverser.SetLastUpdateTime(time.Now().UTC())
		if err != nil {
			return nil, false, fmt.Errorf("failed to set last update time, %w", err)
		}

		err = traverser.SetPlatform(platform)
		if err != nil {
			return nil, false, fmt.Errorf("failed to set platform %w", err)
		}
		newTraverser = true
	}

	return traverser, newTraverser, nil
}

// performEntryAction handles the logic of switching states and calling the Entry function.
//
// It is handled via this function, as a state can manually switch states in the Entry function.
// If that occurs, we then perform the Entry function of that state.  This continues until we land
// in a state whose Entry action doesn't shift us to a new state.
func performEntryAction(state *State, emitter Emitter, traverser Traverser, stateMap StateMap) error {
	err := state.Entry(false)
	if err != nil {
		return err
	}

	// If we switch states in Entry action, we want to perform
	// the next states Entry action.
	currentState, err := traverser.CurrentState()
	if err != nil {
		return fmt.Errorf("failed to get the traversers current state, %w", err)
	}

	if currentState != state.Slug {
		shift, ok := stateMap[currentState]
		if !ok {
			return fmt.Errorf("state (%s) does not exist", currentState)
		}
		shiftedState := shift(emitter, traverser)
		err = performEntryAction(shiftedState, emitter, traverser, stateMap)
		if err != nil {
			return fmt.Errorf("failed to perform recursive entry action, %w", err)
		}
	}
	return nil
}
