package crawlbot

import (
	"fmt"
	"log"
	"runtime"
	"testing"
)

var pagecount int

func TestCrawler(t *testing.T) {
	runtime.GOMAXPROCS(3)

	crawler := Crawler{
		URLs:       []string{"http://example.com", "http://cnn.com", "http://en.wikipedia.org"},
		NumWorkers: 12,
		Handler:    PrintTitle,
		CheckURL:   AllowEverything,
	}
	crawler.Start()
	crawler.Wait()
}

// Print the title of the page
func PrintTitle(resp *Response) {
	if resp.Err != nil {
		log.Println(resp.Err)
	}

	if resp.Doc != nil {
		title, err := resp.Doc.Search("//title")
		if err != nil {
			log.Println(err)
		}
		fmt.Printf("Title of %s is %s\n", resp.URL, title[0].Content())
	} else {
		fmt.Println("HTML was not parsed for " + resp.URL)
	}
}

// Crawl everything!
func AllowEverything(crawler *Crawler, url string) bool {
	return true
}
