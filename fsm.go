package fsm

import "time"

// StartState is a constant for defining the slug of
// the start state for all StateMachines.
const StartState = "start"

// StateMachine is an array of all BuildState functions
type StateMachine []BuildState

// StateMap is a k:v map for all BuildState functions
// in a StateMachine.  This is exclusively utilized
// by the internal workings of targets.
type StateMap map[string]BuildState

// BuildState is a function that generates a State
// with access to a specific Emitter and Traverser
type BuildState func(Emitter, Traverser) *State

// State represents an individual state in a larger state machine
type State struct {
	Slug         string
	IsExitable bool
	Entry        func(isReentry bool) error
	ValidIntents func() []*Intent
	Transition   func(*Intent, map[string]string) *State
}

// Emitter is a generic interface to output arbitrary data.
// Emit is generally called from State.EntryAction.
type Emitter interface {
	Emit(interface{}) error
}

// A Store is a generic interface responsible for managing
// The fetching and creation of traversers
type Store interface {
	FetchTraverser(uuid string) (Traverser, error)
	CreateTraverser(uuid string) (Traverser, error)
}

// A Traverser is an individual that is traversing the
// StateMachine.  This interface that is responsible
// for managing the state of that individual
type Traverser interface {
	// UUID
	UUID() (string, error)
	SetUUID(string) error

	// Platform
	Platform() (string, error)
	SetPlatform(string) error

	// State
	GetLastUpdateTime() (time.Time, error)
	SetLastUpdateTime(t time.Time) error
	CurrentState() (string, error)
	SetCurrentState(string) error

	// Queue
	// Note invoking queued states must be done manually
	AddQueuedState(state string, info interface{}) error
	DequeueQueuedState() error

	// Data
	Upsert(key string, value interface{}) error
	Fetch(key string) (interface{}, error)
	Delete(key string) error
}
