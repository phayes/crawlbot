package crawlbot

import (
	"errors"
	"github.com/moovweb/gokogiri/html"
	"mime"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type State int

// URL states
const (
	StateNotFound State = iota
	StatePending  State = iota
	StateRunning  State = iota
	StateRejected State = iota
	StateDone     State = iota
)

type Response struct {
	Resp    *http.Response
	URL     string
	Error   error
	Crawler *Crawler
	Doc     *html.HtmlDocument
	Bytes   []byte
}

type Crawler struct {
	URLs        []string                  // A list of URLs to start crawling. This is your list of seed URLs
	NumWorkers  int                       // Number of concurrent workers
	Handler     HandlerFunc               // Each page will be passed to this handler. Put your business logic here.
	CheckURL    CheckURLFunc              // Before a URL is crawled it is passed to this function to see if it should be followed or not. By default we follow the link if it's in the same domain as our seed URLs.
	CheckHeader CheckHeaderFunc           // Before reading in the body we can check the headers to see if we want to continue. By default we abort if it's not HTML.
	LinkFinder  LinkFinderFunc            // Call this function to find new links in the document to crawl. By default it will find all <a href> links
	Client      ClientFunc                // The crawler will call this function when it needs a new http.Client to give to a worker. By default it uses the built-in net/http Client.
	workers     []worker                  // List of all workers
	urlstate    map[string]State          // List of URLs and their current state.
	urlindex    map[State]map[string]bool // Index of URLs by their state
	urlmux      sync.RWMutex              // A mutex for protecting urlstate and urlindex
	state       bool                      // True means running. False means stopped.
}

// For each page crawled this function will be called.
// This is where your business logic should reside.
type HandlerFunc func(resp *Response)

// Check to see if the target URL should be crawled.
type CheckURLFunc func(crawler *Crawler, url string) bool

// Check to see if the target should be crawled after inspecting headers
type CheckHeaderFunc func(crawler *Crawler, url string, status int, header http.Header) bool

// Find new URLs to crawl
type LinkFinderFunc func(pres *Response) []string

// Check to see if the target URL should be crawled.
type ClientFunc func() *http.Client

// The default client is the built-in net/http Client with a 15 seconnd timeout
// A sensible alternative might be a simple round-tripper (eg. github.com/pkulak/simpletransport/simpletransport)
// If you wish to rate-throttle your crawler you would do so by implemting a custom http.Client
func DefaultClientFunc() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
	}
}

// The default URL Checker constrains the crawler to the domains of the seed URLs
func DefaultCheckURLFunc(crawler *Crawler, checkurl string) bool {
	parsedURL, err := url.Parse(checkurl)
	if err != nil {
		return false
	}
	for _, seedURL := range crawler.URLs {
		parsedSeed, err := url.Parse(seedURL)
		if err != nil {
			return false
		}
		if parsedSeed.Host == parsedURL.Host {
			return true
		}
	}
	return false
}

// The default header checker will only proceed if it's 200 OK and an HTML Content-Type
func DefaultCheckHeaderFunc(crawler *Crawler, url string, status int, header http.Header) bool {
	if status != 200 {
		return false
	}

	contentType := header.Get("Content-Type")
	if contentType == "" {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return true
	} else {
		return false
	}
}

// Create a new simple crawler
// If more customization options are needed then a Craweler{} should be created directly.
func NewCrawler(url string, handler HandlerFunc, numworkers int) *Crawler {
	return &Crawler{URLs: []string{url}, Handler: handler, CheckURL: DefaultCheckURLFunc, NumWorkers: numworkers, Client: DefaultClientFunc}
}

func (c *Crawler) Start() error {
	if c.state {
		return errors.New("Cannot start crawler that is already running")
	} else {
		c.state = true
	}

	if c.NumWorkers <= 0 {
		panic("Cannot create a new crawler with zero workers")
	}

	// Initialize urlstate and the starting URLs
	c.initializeURLs()

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
	for {
		select {
		case res := <-results:
			c.urlmux.Lock()

			c.urlstate[res.url] = StateDone
			delete(c.urlindex[StateRunning], res.url)
			c.urlindex[StateDone][res.url] = true

			res.owner.teardown()

			if res.err == nil {
				// Add the new items to our map
				for _, newurl := range res.newurls {
					if _, ok := c.urlstate[newurl]; ok {
						continue // Ignore URLs we already have
					}
					if c.CheckURL(c, newurl) {
						c.urlstate[newurl] = StatePending
						c.urlindex[StatePending][newurl] = true
					} else {
						c.urlstate[newurl] = StateRejected
						c.urlindex[StateRejected][newurl] = true
					}
				}
			}

			// Assign more work to the worker
			// If there's no work to do then skip
			if len(c.urlindex[StatePending]) == 0 {
				c.urlmux.Unlock()
				continue // continue select
			}

			c.assignWork(res.owner)
			c.urlmux.Unlock()
		default:
			c.urlmux.Lock()
			if len(c.urlindex[StatePending]) == 0 && len(c.urlindex[StateRunning]) == 0 {
				// We're done
				c.state = false
				c.urlmux.Unlock()
				return nil
			} else if len(c.urlindex[StatePending]) != 0 {
				for i := range c.workers {
					if !c.workers[i].state {
						c.assignWork(&c.workers[i])
						break
					}
				}
				c.urlmux.Unlock()
			} else {
				c.urlmux.Unlock()
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

// Is the crawler currently running or is it stopped?
func (c *Crawler) IsRunning() bool {
	return c.state
}

// Add a URL to the crawler.
// If the item already exists this is a no-op
// @@TODO: change this behavior so an item is re-queued if it already exists -- tricky if the item is StateRunning
func (c *Crawler) AddURL(url string) {
	c.urlmux.Lock()
	if _, ok := c.urlstate[url]; ok {
		return
	}
	c.urlstate[url] = StatePending
	c.urlindex[StatePending][url] = true
	c.urlmux.Unlock()
}

// Get the current state for a URL
func (c *Crawler) GetURL(url string) State {
	c.urlmux.RLock()
	defer c.urlmux.RUnlock()

	state, ok := c.urlstate[url]
	if !ok {
		return StateNotFound
	}

	return state
}

// Assign work to a worker. Calling this function is unsafe unless wrapped inside a mutex lock
func (c *Crawler) assignWork(w *worker) {
	for url := range c.urlindex[StatePending] {
		c.urlstate[url] = StateRunning

		// Update the index
		delete(c.urlindex[StatePending], url)
		c.urlindex[StateRunning][url] = true

		// Assign work and return true
		w.setup(url)
		w.process()
		break
	}
}

// Build the index.
func (c *Crawler) initializeURLs() {
	c.urlmux.Lock()

	if c.urlstate == nil {
		c.urlstate = make(map[string]State)
	}
	for _, url := range c.URLs {
		if _, ok := c.urlstate[url]; !ok {
			c.urlstate[url] = StatePending
		}
	}

	// Build the index
	c.urlindex = make(map[State]map[string]bool)
	for _, state := range []State{StatePending, StateRejected, StateRunning, StateDone} {
		c.urlindex[state] = make(map[string]bool)
	}

	for url, state := range c.urlstate {
		c.urlindex[state][url] = true
	}

	c.urlmux.Unlock()
}
