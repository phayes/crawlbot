package crawlbot

import (
	"errors"
	"github.com/moovweb/gokogiri/html"
	"github.com/moovweb/gokogiri/xml"
	"github.com/moovweb/gokogiri/xpath"
	"io/ioutil"
	"mime"
	"net/http"
	"strings"
)

var xa = xpath.Compile(".//a")

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
			resp.Body.Close()
			resp.Err = ErrHeaderRejected
			w.sendResults(nil, resp.Err)
			return
		}

		// Read the body
		resp.Bytes, resp.Err = ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.Err != nil {
			w.crawler.Handler(&resp)
			w.sendResults(nil, resp.Err)
			return
		}

		// Parse the HTML / XML
		if contentType := resp.Header.Get("Content-Type"); contentType != "" {
			mediaType, _, err := mime.ParseMediaType(contentType)
			if err == nil {
				if mediaType == "text/html" {
					var htmldoc *html.HtmlDocument
					htmldoc, resp.Err = html.Parse(resp.Bytes, html.DefaultEncodingBytes, []byte(w.url), html.DefaultParseOption, html.DefaultEncodingBytes)
					resp.Doc = htmldoc.XmlDocument
				} else if mediaType == "application/xml" || mediaType == "text/xml" || strings.HasSuffix(mediaType, "+xml") {
					resp.Doc, resp.Err = xml.Parse(resp.Bytes, html.DefaultEncodingBytes, []byte(w.url), html.DefaultParseOption, html.DefaultEncodingBytes)
				}
				defer resp.Doc.Free()
			}
		}

		// Process the handler
		w.crawler.Handler(&resp)

		// Find links and finish
		newurls := w.crawler.LinkFinder(&resp)

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
