package crawlbot

import (
	"errors"
	"github.com/moovweb/gokogiri/xml"
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

// When handling a crawled page a Response is passed to the Handler function.
// A crawlbot.Response is an http.Response with a few extra fields.
type Response struct {
	// The http.Reponse object
	// Do not read from Body as it has already been consumed and closed, instead use Response.Bytes
	*http.Response

	// The for this Response
	URL string

	// If any errors were encountered in retrieiving or processing this item, Err will be non-nill
	// Your Handler function should generally check this first
	Err error

	// The Crawler object that retreived this item. You may use this to stop the crawler, add more urls etc.
	// Calling Crawler.Wait() from within your Handler will cause a deadlock. Don't do this.
	Crawler *Crawler

	// Parsed gokogiri XML Document. It will be parsed using an HTML or XML parser depending on the Content Type
	// This will be nil if the document was not recognized as html or xml
	Doc *xml.XmlDocument

	// The Body of the http.Reponse has already been consumed by the time the response is passed to Handler.
	// Instead of reading from Body you should use Response.Bytes.
	Bytes []byte
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

	workers  []worker                  // List of all workers
	urlstate map[string]State          // List of URLs and their current state.
	urlindex map[State]map[string]bool // Index of URLs by their state
	urlmux   sync.RWMutex              // A mutex for protecting urlstate and urlindex
	state    bool                      // True means running. False means stopped.
}

// The default URL Checker constrains the crawler to the domains of the seed URLs
func DefaultCheckURL(crawler *Crawler, checkurl string) bool {
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
func DefaultCheckHeader(crawler *Crawler, url string, status int, header http.Header) bool {
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

// The default link finder finds all <a href> links in an HMTL document
func DefaultLinkFinder(resp *Response) []string {
	var newurls = make([]string, 0)

	// If the document couldn't be parsed, there's nothing to do
	if resp.Doc == nil {
		return newurls
	}

	alinks, err := resp.Doc.Search(xa)
	if err != nil {
		return newurls
	}

	parsedURL, err := url.Parse(resp.URL)
	if err != nil {
		return newurls
	}

	for _, alink := range alinks {
		link := alink.Attr("href")
		parsedLink, err := url.Parse(link)
		if err != nil {
			continue
		}
		absLink := parsedURL.ResolveReference(parsedLink)
		newurls = append(newurls, absLink.String())
	}

	return newurls
}

// The default client is the built-in net/http Client with a 15 second timeout
func DefaultClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
	}
}

// Create a new simple crawler.
// If more customization options are needed then a Crawler{} should be created directly.
func NewCrawler(url string, handler func(resp *Response), numworkers int) *Crawler {
	return &Crawler{URLs: []string{url}, Handler: handler, NumWorkers: numworkers}
}

func (c *Crawler) Start() error {
	// Check to see if the crawler is already running
	if c.state {
		return errors.New("Cannot start crawler that is already running")
	} else {
		c.state = true
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
		c.CheckHeader = DefaultCheckHeader
	}
	if c.CheckURL == nil {
		c.CheckURL = DefaultCheckURL
	}
	if c.LinkFinder == nil {
		c.LinkFinder = DefaultLinkFinder
	}
	if c.Client == nil {
		c.Client = DefaultClient
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
	go func() {
		for {
			select {
			case res := <-results:
				c.urlmux.Lock()

				res.owner.teardown()

				if res.err == ErrHeaderRejected {
					c.urlstate[res.url] = StateRejected
					delete(c.urlindex[StateRunning], res.url)
					c.urlindex[StateRejected][res.url] = true
				} else {
					c.urlstate[res.url] = StateDone
					delete(c.urlindex[StateRunning], res.url)
					c.urlindex[StateDone][res.url] = true
				}

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
				// If there's no work to do or we're supposex to stop then skip
				if len(c.urlindex[StatePending]) == 0 || !c.state {
					c.urlmux.Unlock()
					continue // continue select
				}

				c.assignWork(res.owner)
				c.urlmux.Unlock()
			default:
				c.urlmux.Lock()
				// If there is nothing running and either we have nothing pending or we are in a stopped state, then we're done
				if len(c.urlindex[StateRunning]) == 0 && (len(c.urlindex[StatePending]) == 0 || !c.state) {
					// We're done
					c.state = false
					c.urlmux.Unlock()
					return
				} else if len(c.urlindex[StatePending]) != 0 && c.state {
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
	}()

	return nil
}

// Is the crawler currently running or is it stopped?
func (c *Crawler) IsRunning() bool {
	return c.state
}

// Stop a running crawler. This stops all new work but doesn't cancel ongoing jobs
// After calling Stop(), call Wait() to wait for everything to finish
func (c *Crawler) Stop() {
	c.state = false
}

// Wait for the crawler to finish
// Calling this within a Handler function will cause a deadlock. Don't do this.
func (c *Crawler) Wait() {
	for {
		c.urlmux.RLock()
		numRunning := len(c.urlindex[StateRunning])
		c.urlmux.RUnlock()
		if numRunning == 0 && c.state == false {
			return
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
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
