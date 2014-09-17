CrawlBot
========

Crawlbot is a simple, efficient, and flexible webcrawler. Crawlbot is easy to use out-of-the-box, but also provides extensive flexibility for advanced users.

```go
package main

import (
  "github.com/phayes/crawlbot"
  "log"
  "fmt"
)

func main() {
  crawler := NewCrawler("http://cnn.com", myURLHandler, 4)
  crawler.Start()
}

func myURLHandler (resp *Response) {
  if resp.Error != nil {
    log.Fatal(resp.Error)
  }
  
  fmt.Println("Found URL at " + resp.URL)
}
```
