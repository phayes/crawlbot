package crawlbot

import (
	"sync"
)

type urls struct {
	sync.RWMutex                           // A mutex for protecting urls and urlindex
	urls         map[string]State          // List of URLs and their current state.
	index        map[State]map[string]bool // Index of URLs by their state
}

func NewUrls(seeds []string) *urls {
	u := urls{
		urls:  make(map[string]State),
		index: make(map[State]map[string]bool),
	}

	// Initialize with seeds urls
	for _, seed := range seeds {
		u.urls[seed] = StatePending
	}

	// build the index
	u.buildIndex()

	return &u
}

// Rebuild the index
func (u *urls) buildIndex() {
	u.Lock()
	defer u.Unlock()

	for _, state := range []State{StatePending, StateRejected, StateRunning, StateDone} {
		u.index[state] = make(map[string]bool)
	}
	for url, state := range u.urls {
		u.index[state][url] = true
	}
}

// Add new urls to our url list.
// If an item already exists it's a no-op
func (u *urls) add(urls []string) {
	u.Lock()
	defer u.Unlock()

	for _, url := range urls {
		if _, ok := u.urls[url]; ok {
			continue
		}
		u.urls[url] = StatePending
		u.index[StatePending][url] = true
	}
}

// Change the state of a URL.
// Will panic if url does not exist
func (u *urls) changeState(url string, state State) {
	u.Lock()
	defer u.Unlock()

	oldstate, ok := u.urls[url]
	if !ok {
		panic("Cannot change state of url that does not exist.")
	}
	u.urls[url] = state
	delete(u.index[oldstate], url)
	u.index[state][url] = true
}

// Get a URL state
func (u *urls) state(url string) State {
	u.RLock()
	defer u.RUnlock()

	state, ok := u.urls[url]
	if !ok {
		return StateNotFound
	}

	return state
}

// Get the number of URls in a given state
func (u *urls) numstate(state State) int {
	u.Lock()
	defer u.Unlock()

	return len(u.index[state])
}

// Select a random URL that is pending, move it to a running state, and return the select url
func (u *urls) selectPending() (url string, ok bool) {
	u.Lock()
	defer u.Unlock()

	if len(u.index[StatePending]) == 0 {
		return "", false
	}

	for url = range u.index[StatePending] {
		u.urls[url] = StateRunning
		delete(u.index[StatePending], url)
		u.index[StateRunning][url] = true

		return url, true
	}
	return "", false
}
