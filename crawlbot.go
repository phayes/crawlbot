package crawlbot

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

type State int

// URL states.
// You can query the current state of a url by calling Crawler.GetURL(url)
const (
	StateNotFound State = iota
	StatePending  State = iota
	StateRunning  State = iota
	StateRejected State = iota
	StateDone     State = iota
)

// When handling a crawled page a Response is passed to the Handler function.
// A crawlbot.Response is an http.Response with a few extra fields.
type Response struct {
	// The http.Reponse object
	*http.Response

	// The for this Response
	URL string

	// If any errors were encountered in retrieiving or processing this item, Err will be non-nill
	// Your Handler function should generally check this first
	Err error

	// The Crawler object that retreived this item. You may use this to stop the crawler, add more urls etc.
	// Calling Crawler.Wait() from within your Handler will cause a deadlock. Don't do this.
	Crawler *Crawler

	// The Body of the http.Reponse has already been consumed by the time the response is passed to Handler.
	// bytes contains the read Body
	bytes []byte
}

type Crawler struct {
	// A list of URLs to start crawling. This is your list of seed URLs.
	URLs []string

	// Number of concurrent workers
	NumWorkers int

	// For each page crawled this function will be called.
	// This is where your business logic should reside.
	// There is no default. If Handler is not set the crawler will panic.
	Handler func(resp *Response)

	// Before a URL is crawled it is passed to this function to see if it should be followed or not.
	// By default we follow the link if it's in one of the same domains as our seed URLs.
	CheckURL func(crawler *Crawler, url string) bool

	// Before reading in the body we can check the headers to see if we want to continue.
	// By default we abort if it's not HTTP 200 OK or not an html Content-Type.
	// Override this function if you wish to handle non-html files such as binary images
	CheckHeader func(crawler *Crawler, url string, status int, header http.Header) bool

	// This function is called to find new urls in the document to crawl. By default it will
	// find all <a href> links in an html document. Override this function if you wish to follow
	// non <a href> links such as <img src>, or if you wish to find links in non-html documents.
	LinkFinder func(resp *Response) []string

	// The crawler will call this function when it needs a new http.Client to give to a worker.
	// The default client is the built-in net/http Client with a 15 seconnd timeout
	// A sensible alternative might be a simple round-tripper (eg. github.com/pkulak/simpletransport/simpletransport)
	// If you wish to rate-throttle your crawler you would do so by implemting a custom http.Client
	Client func() *http.Client

	// Set this to true and the crawler will not stop by itself, you will need to explicitly call Stop()
	// This is useful when you need a long-running crawler that you occationally feed new urls via Add()
	Persistent bool

	workers  []worker   // List of all workers
	running  bool       // True means running. False means stopped.
	mux      sync.Mutex // A mutex to coordiate starting and stopping the crawler
	urlstate *urls      // Ongoing working set of URLs
}

// Create a new simple crawler.
// If more customization options are needed then a Crawler{} should be created directly.
func NewCrawler(url string, handler func(resp *Response), numworkers int) *Crawler {
	return &Crawler{URLs: []string{url}, Handler: handler, NumWorkers: numworkers}
}

// Start crawling. Start() will immidiately return; if you wish to wait for the crawl to finish
// you will want to cal Wait() after calling Start().
func (c *Crawler) Start() error {
	c.mux.Lock()
	defer c.mux.Unlock()

	// Check to see if the crawler is already running
	if c.running {
		return errors.New("Cannot start crawler that is already running")
	} else {
		c.running = true
	}

	// Sanity check
	if c.NumWorkers <= 0 {
		panic("Cannot create a new crawler with zero workers")
	}
	if c.Handler == nil {
		panic("Cannot start a crawler that doesn't have a Hanlder function.")
	}
	if len(c.URLs) == 0 {
		panic("Cannot start a crawler with no URLs.")
	}

	// Initialize the default functions
	if c.CheckHeader == nil {
		c.CheckHeader = defaultCheckHeader
	}
	if c.CheckURL == nil {
		c.CheckURL = defaultCheckURL
	}
	if c.LinkFinder == nil {
		c.LinkFinder = defaultLinkFinder
	}
	if c.Client == nil {
		c.Client = defaultClient
	}

	// Initialize urlstate and the starting URLs
	if c.urlstate == nil {
		c.urlstate = newUrls(c.URLs)
	} else {
		// If it's already initialized, just rebuild the index
		c.urlstate.buildIndex()
	}

	// Initialize worker communication channels
	results := make(chan result)

	// Initialize workers
	c.workers = make([]worker, c.NumWorkers)
	for i := range c.workers {
		c.workers[i].crawler = c
		c.workers[i].results = results
		c.workers[i].client = c.Client()
	}

	// Start running in a for loop with selects
	go func() {
		for {
			select {
			case res := <-results:
				c.processResult(res)
			default:
				c.mux.Lock()
				// If there is nothing running and either we have nothing pending or we are in a stopped state, then we're done
				if c.urlstate.numstate(StateRunning) == 0 && (c.urlstate.numstate(StatePending) == 0 || !c.running) {
					// We're done
					c.running = false
					c.mux.Unlock()
					return
				} else if c.urlstate.numstate(StatePending) != 0 && c.running {
					for i := range c.workers {
						if !c.workers[i].state {
							newurl, ok := c.urlstate.selectPending()
							if !ok {
								panic("No pending urls to process despite numstate reporting available pending items")
							}
							c.workers[i].setup(newurl)
							c.workers[i].process()
							break
						}
					}
					c.mux.Unlock()
				} else {
					c.mux.Unlock()
					time.Sleep(100 * time.Millisecond)
				}
			}
		}
	}()

	return nil
}

// Is the crawler currently running or is it stopped?
func (c *Crawler) IsRunning() bool {
	c.mux.Lock()
	defer c.mux.Unlock()

	return c.running
}

// Stop a running crawler. This stops all new work but doesn't cancel ongoing jobs.
// After calling Stop(), call Wait() to wait for everything to finish
func (c *Crawler) Stop() {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.running = false
}

// Wait for the crawler to finish, blocking until it's done.
// Calling this within a Handler function will cause a deadlock. Don't do this.
func (c *Crawler) Wait() {
	for {
		c.mux.Lock()
		if c.urlstate.numstate(StateRunning) == 0 && c.running == false {
			c.mux.Unlock()
			return
		} else {
			c.mux.Unlock()
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// Add a URL to the crawler.
// If the item already exists this is a no-op.
// TODO: change this behavior so an item is re-queued if it already exists -- tricky if the item is StateRunning
func (c *Crawler) Add(url string) {
	c.urlstate.add([]string{url})
}

// Get the current state for a URL.
func (c *Crawler) State(url string) State {
	return c.urlstate.state(url)
}

func (c *Crawler) processResult(res result) {
	c.mux.Lock()
	defer c.mux.Unlock()

	res.owner.teardown()

	if res.err == ErrHeaderRejected {
		c.urlstate.changeState(res.url, StateRejected)
	} else {
		c.urlstate.changeState(res.url, StateDone)
	}

	if res.err == nil {
		c.urlstate.add(res.newurls)
	}

	// Assign more work to the worker if we are running
	if c.running {
		newurl, ok := c.urlstate.selectPending()
		if ok {
			res.owner.setup(newurl)
			res.owner.process()
		}
	}
}
