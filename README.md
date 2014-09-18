CrawlBot
========

[![GoDoc](https://godoc.org/github.com/phayes/crawlbot?status.svg)](https://godoc.org/github.com/phayes/crawlbot)

CrawlBot is a simple, efficient, and flexible webcrawler / spider. CrawlBot is easy to use out-of-the-box, but also provides extensive flexibility for advanced users.

```go
package main

import (
	"fmt"
	"github.com/phayes/crawlbot"
	"log"
)

func main() {
	crawler := NewCrawler("http://cnn.com", myURLHandler, 4)
	crawler.Start()
	crawler.Wait()
}

func myURLHandler(resp *crawlbot.Response) {
	if resp.Err != nil {
		log.Fatal(resp.Err)
	}

	fmt.Println("Found URL at " + resp.URL)
}
```

CrawlBot provides extensive customizability for advances use cases. Please see documentation on [crawlbot.Crawler](https://godoc.org/github.com/phayes/crawlbot#Crawler) and [crawlbot.Response](https://godoc.org/github.com/phayes/crawlbot#Response) for more details.

```go
package main

import (
	"fmt"
	"github.com/phayes/crawlbot"
	"log"
)

func main() {
	crawler := crawlbot.Crawler{
		URLs:       []string{"http://example.com", "http://cnn.com", "http://en.wikipedia.org"},
		NumWorkers: 12,
		Handler:    PrintTitle,
		CheckURL:   AllowEverything,
	}
	crawler.Start()
	crawler.Wait()
}

// Print the title of the page
func PrintTitle(resp *crawlbot.Response) {
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
func AllowEverything(crawler *crawlbot.Crawler, url string) bool {
	return true
}

```
