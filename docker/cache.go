package docker

import (
	"sync"
	"time"
)

type ContainerState struct {
	Running    bool
	Paused     bool
	LastUsed   *time.Time
	IPAddress  string
	LastUpdate *time.Time
}

var containerStates map[string]ContainerState = map[string]ContainerState{}
var containerStateMutext sync.RWMutex

func isRegisteredUnsafe(name string) bool {
	_, found := containerStates[name]
	return found
}

func IsRegistered(name string) bool {
	containerStateMutext.RLock()
	defer containerStateMutext.RUnlock()

	_, found := containerStates[name]
	return found
}

func GetState(name string) (ContainerState, error) {
	containerStateMutext.RLock()
	defer containerStateMutext.RUnlock()

	state, found := containerStates[name]
	if !found {
		return state, ErrorNotRegistered
	}
	return state, nil
}

type Pair struct {
	Name      string
	Container ContainerState
}

func IterContainers(yield func(name string, container ContainerState) bool) {
	containerStateMutext.RLock()
	defer containerStateMutext.RUnlock()

	for k, v := range containerStates {
		if !yield(k, v) {
			return
		}
	}
}

func IsStale(name string) bool {
	containerStateMutext.RLock()
	defer containerStateMutext.RUnlock()

	container, found := containerStates[name]
	if !found {
		return true
	}
	if container.LastUpdate == nil {
		return true
	}
	return time.Now().After((*container.LastUpdate).Add(60 * time.Second))
}

func RegisterContainer(name string) {
	containerStateMutext.Lock()
	defer containerStateMutext.Unlock()

	if !isRegisteredUnsafe(name) {
		state := ContainerState{}
		containerStates[name] = state
	}
}

func updateState(name string, running bool, paused bool, ip string) error {
	containerStateMutext.Lock()
	defer containerStateMutext.Unlock()

	state, found := containerStates[name]
	if !found {
		return ErrorNotRegistered
	}
	state.Running = running
	state.Paused = paused
	state.IPAddress = ip
	t := time.Now()
	state.LastUpdate = &t
	containerStates[name] = state

	return nil
}
func updateRunning(name string, running bool, paused bool) error {
	containerStateMutext.Lock()
	defer containerStateMutext.Unlock()

	state, found := containerStates[name]
	if !found {
		return ErrorNotRegistered
	}
	state.Running = running
	state.Paused = paused
	t := time.Now()
	state.LastUpdate = &t
	containerStates[name] = state

	return nil
}
func updatePaused(name string, paused bool) error {
	containerStateMutext.Lock()
	defer containerStateMutext.Unlock()

	state, found := containerStates[name]
	if !found {
		return ErrorNotRegistered
	}
	state.Paused = paused
	t := time.Now()
	state.LastUpdate = &t
	containerStates[name] = state

	return nil
}

func UpdateUsed(name string) error {
	containerStateMutext.Lock()
	defer containerStateMutext.Unlock()

	state, found := containerStates[name]
	if !found {
		return ErrorNotRegistered
	}
	t := time.Now()
	state.LastUsed = &t

	containerStates[name] = state

	return nil
}
