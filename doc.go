/*
Crawlbot is a simple, efficient, and flexible webcrawler. Crawlbot is easy to use out-of-the-box, but also provides extensive flexibility for advanced users.

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

CrawlBot provides extensive customizability for advances use cases. Please see documentation on Crawler and Response for more details.

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
*/
package crawlbot
