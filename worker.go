package crawlbot

import (
	"github.com/moovweb/gokogiri/html"
	"github.com/moovweb/gokogiri/xpath"
	"io/ioutil"
	"net/http"
	"net/url"
)

var xa = xpath.Compile(".//a")

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
		// Parse the URL to ensure it's valid
		parsedURL, err := url.Parse(w.url)
		if err != nil {
			w.sendResults(nil, err)
			return
		}

		// Do the HTTP GET and create the response object
		resphttp, err := w.client.Get(w.url)
		resp := Response{
			Resp:    resphttp,
			URL:     w.url,
			Error:   err,
			Crawler: w.crawler,
		}
		if resp.Error != nil {
			w.crawler.Handler(&resp)
			w.sendResults(nil, resp.Error)
			return
		}

		// Read the body
		resp.Bytes, resp.Error = ioutil.ReadAll(resphttp.Body)
		resphttp.Body.Close()
		if resp.Error != nil {
			w.crawler.Handler(&resp)
			w.sendResults(nil, resp.Error)
			return
		}

		// Parse the HTML
		resp.Doc, resp.Error = html.Parse(resp.Bytes, html.DefaultEncodingBytes, []byte(w.url), html.DefaultParseOption, html.DefaultEncodingBytes)
		defer resp.Doc.Free()
		if resp.Error != nil {
			w.crawler.Handler(&resp)
			w.sendResults(nil, resp.Error)
			return
		}

		// Process the handler
		w.crawler.Handler(&resp)

		// Find links and finish
		// @@TODO - Make this customizable but leave this behavior as default
		var newurls = make([]string, 0)
		alinks, err := resp.Doc.Search(xa)
		if err != nil {
			w.sendResults(nil, err)
			return
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
