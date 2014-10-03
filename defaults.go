package crawlbot

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/phayes/errors"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// The default URL Checker constrains the crawler to the domains of the seed URLs
func defaultCheckURL(crawler *Crawler, checkurl string) error {
	parsedURL, err := url.Parse(checkurl)
	if err != nil {
		return err
	}
	for _, seedURL := range crawler.URLs {
		parsedSeed, err := url.Parse(seedURL)
		if err != nil {
			return err
		}
		if parsedSeed.Host == parsedURL.Host {
			return nil
		}
	}
	return errors.New("URL not in approved domain")
}

// The default header checker will only proceed if it's 200 OK and an HTML Content-Type
func defaultCheckHeader(crawler *Crawler, url string, status int, header http.Header) error {
	if status != 200 {
		return errors.Appends(ErrBadHttpCode, "Received "+strconv.Itoa(status)+" "+http.StatusText(status))
	}

	contentType := header.Get("Content-Type")
	if contentType == "" {
		return errors.Appends(ErrBadContentType, "Content-Type header missing")
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return errors.Appends(ErrBadContentType, "Malformated Content-Type header")
	}

	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return nil
	} else {
		return errors.Appends(ErrBadContentType, mediaType+" is not supported")
	}
}

// The default link finder finds all <a href> links in an HMTL document
func defaultLinkFinder(resp *Response) []string {
	var newurls = make([]string, 0)

	if defaultCheckHeader(resp.Crawler, resp.URL, resp.StatusCode, resp.Header) != nil {
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

	doc.Find("a:not([rel='nofollow'])").Each(func(i int, s *goquery.Selection) {
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
