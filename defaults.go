package crawlbot

import (
	"github.com/PuerkitoBio/goquery"
	"mime"
	"net/http"
	"net/url"
	"time"
)

// The default URL Checker constrains the crawler to the domains of the seed URLs
func defaultCheckURL(crawler *Crawler, checkurl string) bool {
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
func defaultCheckHeader(crawler *Crawler, url string, status int, header http.Header) bool {
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
func defaultLinkFinder(resp *Response) []string {
	var newurls = make([]string, 0)

	if !defaultCheckHeader(resp.Crawler, resp.URL, resp.StatusCode, resp.Header) {
		return newurls
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return newurls
	}

	parsedURL, err := url.Parse(resp.URL)
	if err != nil {
		return newurls
	}

	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		link, ok := s.Attr("href")
		if ok {
			parsedLink, err := url.Parse(link)
			parsedLink.Fragment = "" // Unset the #fragment if it exists
			if err == nil {
				absLink := parsedURL.ResolveReference(parsedLink)
				newurls = append(newurls, absLink.String())
			}
		}
	})

	return newurls
}

// The default client is the built-in net/http Client with a 15 second timeout
func defaultClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
	}
}
