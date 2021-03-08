package fsm

import "fmt"

const QueueInfoKey string = "queue_info"

// GetStateMap converts a StateMachine into a StateMap
func GetStateMap(stateMachine StateMachine) StateMap {
	stateMap := make(StateMap, 0)
	for _, buildState := range stateMachine {
		stateMap[buildState(nil, nil).Slug] = buildState
	}
	return stateMap
}

func EnqueueStateToUser(platform, uuid, state string, input interface{}, store Store) error {
	// Get Traverser
	traverser, err := store.FetchTraverser(uuid)
	if err != nil {
		traverser, err = store.CreateTraverser(uuid)
		if err != nil {
			return fmt.Errorf("failed to create traverser for id (%s), %w", uuid, err)
		}
		err = traverser.SetCurrentState(StartState)
		if err != nil {
			return fmt.Errorf("failed to set current state to start state %w", err)
		}
		err = traverser.SetPlatform(platform)
		if err != nil {
			return fmt.Errorf("failed to set platform %w", err)
		}
	}

	err = traverser.EnqueueQueuedState(state, input)
	if err != nil {
		return fmt.Errorf("failed to enqueue state to user, %w", err)
	}

	return nil
}

// Step performs a single step through a StateMachine.
//
// This function handles the nuance of the logic for a single step through a state machine.
// ALL fsm-target's should call Step directly, and not attempt to handle the process of stepping through
// the StateMachine, so all platforms function with the same logic.
func Step(platform, uuid string, input interface{}, InputTransformer InputTransformer, store Store, emitter Emitter, stateMap StateMap) error {
	// Get Traverser
	newTraverser := false
	traverser, err := store.FetchTraverser(uuid)
	if err != nil {
		traverser, err = store.CreateTraverser(uuid)
		if err != nil {
			return fmt.Errorf("failed to create traverser for id (%s), %w", uuid, err)
		}
		err = traverser.SetCurrentState(StartState)
		if err != nil {
			return fmt.Errorf("failed to set current state to start state %w", err)
		}
		err = traverser.SetPlatform(platform)
		if err != nil {
			return fmt.Errorf("failed to set platform %w", err)
		}
		newTraverser = true
	}

	// check if traverser has any queued states
	// if there are it will override whatever state was attempted and eventually should lead back to let the user decide
	// this is for matters that the bot initiates generally coming from "triggers"
	if !newTraverser {
		state, info, ok, err := traverser.DequeueQueuedState()
		if err != nil {
			return fmt.Errorf("failed to check queued states, %w", err)
		}

		// if anything was actually dequeued
		if ok {
			err = traverser.SetCurrentState(state)
			if err != nil {
				return fmt.Errorf("failed to set current state from queued state provided (state=%s), %w", state, err)
			}

			err = traverser.Upsert(QueueInfoKey, info)
			if err != nil {
				return fmt.Errorf("failed to upsert queue info, %w", err)
			}
		}
	}

	// Get current state
	traverserCurState, err := traverser.CurrentState()
	if err != nil {
		return fmt.Errorf("failed to get current state from traverser %w", err)
	}
	currentState := stateMap[traverserCurState](emitter, traverser)
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
		shiftedState := stateMap[currentState](emitter, traverser)
		err = performEntryAction(shiftedState, emitter, traverser, stateMap)
		if err != nil {
			return fmt.Errorf("failed to perform recursive entry action, %w", err)
		}
	}
	return nil
}
