package crawlbot

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
)

var ErrHeaderRejected = errors.New("Header Checker rejected URL")

type worker struct {
	state   bool         // true means busy / unavailable. false means idling and is ready for new work
	url     string       // Current URL being processed
	results chan result  // Channel on which to send results
	crawler *Crawler     // It's parent crawler
	client  *http.Client // The client to be used for HTTP connection
}

type result struct {
	err     error
	url     string
	newurls []string
	owner   *worker
}

// Process a given URL, when finish pass back a new list of URLs to process

func (w *worker) setup(targetURL string) {
	w.state = true
	w.url = targetURL
}

func (w *worker) teardown() {
	w.state = false
	w.url = ""
}

func (w *worker) process() {
	go func() {
		// Do the HTTP GET and create the response object
		var resp Response
		httpresp, err := w.client.Get(w.url)
		if httpresp != nil {
			resp = Response{Response: httpresp}
		} else {
			resp = Response{}
		}
		resp.URL = w.url
		resp.Err = err
		resp.Crawler = w.crawler
		if resp.Err != nil {
			w.crawler.Handler(&resp)
			w.sendResults(nil, resp.Err)
			return
		}

		// Check headers using HeaderCheck
		if !w.crawler.CheckHeader(w.crawler, w.url, resp.StatusCode, resp.Header) {
			resp.Err = ErrHeaderRejected
			w.crawler.Handler(&resp)
			resp.Body.Close()
			w.sendResults(nil, resp.Err)
			return
		}

		// Read the body
		resp.bytes, resp.Err = ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.Err != nil {
			w.crawler.Handler(&resp)
			w.sendResults(nil, resp.Err)
			return
		}
		// Replace the body with a readCloser that reads from bytes
		resp.Body = &readCloser{bytes.NewReader(resp.bytes)}

		// Process the handler
		w.crawler.Handler(&resp)
		resp.Body = &readCloser{bytes.NewReader(resp.bytes)}

		// Find links and finish
		newurls := make([]string, 0)
		for _, url := range w.crawler.LinkFinder(&resp) {
			if w.crawler.CheckURL(w.crawler, url) {
				newurls = append(newurls, url)
			}
		}

		// We're done, return the results
		w.sendResults(newurls, nil)
	}()
}

func (w *worker) sendResults(newurls []string, err error) {
	result := result{
		err:     err,
		url:     w.url,
		newurls: newurls,
		owner:   w,
	}

	w.results <- result
}

// ReadCloser is a dummy type that makes bytes.Reader compatible with ReadCloser so we can use it to replace Body
type readCloser struct {
	*bytes.Reader
}

func (r *readCloser) Close() error {
	return nil
}
